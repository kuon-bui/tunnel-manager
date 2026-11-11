package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestRewriteHost(t *testing.T) {
	from := splitSet("localhost,127.0.0.1,::1")
	cases := []struct {
		in, want string
		changed  bool
	}{
		{"http://localhost:3001", "http://10.10.1.10:3001", true},
		{"https://127.0.0.1:8443/app?x=1", "https://10.10.1.10:8443/app?x=1", true},
		{"http://[::1]:9000", "http://10.10.1.10:9000", true},
		{"http://LOCALHOST", "http://10.10.1.10", true},
		{"http://10.0.0.5:3001", "http://10.0.0.5:3001", false},
	}
	for _, tc := range cases {
		got, changed, err := rewriteHost(tc.in, from, "10.10.1.10")
		if err != nil || got != tc.want || changed != tc.changed {
			t.Errorf("rewriteHost(%q) = %q, %v, %v; want %q, %v", tc.in, got, changed, err, tc.want, tc.changed)
		}
	}
}

// fakeBackend mimics the list and routes endpoints, with two list pages.
type fakeBackend struct {
	mu      sync.Mutex
	domains []domain
}

func (f *fakeBackend) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.URL.Path == "/api/auth/login" {
		json.NewEncoder(w).Encode(map[string]string{"token": "tok"})
		return
	}
	if r.Header.Get("Authorization") != "Bearer tok" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/domains":
		if r.URL.Query().Get("cursor") == "" {
			json.NewEncoder(w).Encode(map[string]any{"items": f.domains[:2], "nextCursor": "c1"})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"items": f.domains[2:], "nextCursor": ""})
	case r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/routes"):
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/domains/"), "/routes")
		var body struct {
			Routes []route `json:"routes"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		for i := range f.domains {
			if f.domains[i].ID == id {
				f.domains[i].Routes = body.Routes
			}
		}
		w.Write([]byte("{}"))
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (f *fakeBackend) routes(id string) []route {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, d := range f.domains {
		if d.ID == id {
			return append([]route(nil), d.Routes...)
		}
	}
	return nil
}

func TestRunApplyAndRestore(t *testing.T) {
	aiRoutes := []route{{"/", "http://localhost:20128", false}, {"/tools", "http://localhost:20129", true}}
	apiRoutes := []route{{"/", "http://localhost:8080", false}}
	lanRoutes := []route{{"/", "http://10.0.0.5:80", false}}
	fb := &fakeBackend{domains: []domain{
		{ID: "ai", Hostname: "ai.example.com", Routes: aiRoutes},
		{ID: "api", Hostname: "api.example.com", Routes: apiRoutes},
		{ID: "lan", Hostname: "lan.example.com", Routes: lanRoutes},
	}}
	srv := httptest.NewServer(fb)
	defer srv.Close()

	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("TM_PASSWORD", "pw")
	base := options{api: srv.URL, user: "admin", from: "localhost,127.0.0.1", to: "10.10.1.10", exclude: "api.example.com"}

	if err := run(base); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fb.routes("ai"), aiRoutes) {
		t.Fatalf("dry run modified routes: %v", fb.routes("ai"))
	}

	apply := base
	apply.apply = true
	if err := run(apply); err != nil {
		t.Fatal(err)
	}
	wantAI := []route{{"/", "http://10.10.1.10:20128", false}, {"/tools", "http://10.10.1.10:20129", true}}
	if got := fb.routes("ai"); !reflect.DeepEqual(got, wantAI) {
		t.Fatalf("ai routes after apply = %v, want %v", got, wantAI)
	}
	if got := fb.routes("api"); !reflect.DeepEqual(got, apiRoutes) {
		t.Fatalf("excluded domain changed: %v", got)
	}
	if got := fb.routes("lan"); !reflect.DeepEqual(got, lanRoutes) {
		t.Fatalf("non-local domain changed: %v", got)
	}

	backups, _ := filepath.Glob(filepath.Join(dir, "origins-backup-*.json"))
	if len(backups) != 1 {
		t.Fatalf("want 1 backup file, got %v", backups)
	}

	if err := run(options{api: srv.URL, user: "admin", restore: backups[0], apply: true}); err != nil {
		t.Fatal(err)
	}
	if got := fb.routes("ai"); !reflect.DeepEqual(got, aiRoutes) {
		t.Fatalf("restore did not revert: %v", got)
	}
}
