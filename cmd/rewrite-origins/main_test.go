package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRewriteHost(t *testing.T) {
	from := splitSet("localhost,127.0.0.1,::1")
	cases := []struct {
		in, want string
		changed  bool
	}{
		{"http://localhost:3001", "http://192.168.1.50:3001", true},
		{"https://127.0.0.1:8443/app?x=1", "https://192.168.1.50:8443/app?x=1", true},
		{"http://[::1]:9000", "http://192.168.1.50:9000", true},
		{"http://LOCALHOST", "http://192.168.1.50", true},
		{"http://10.0.0.5:3001", "http://10.0.0.5:3001", false},
	}
	for _, tc := range cases {
		got, changed, err := rewriteHost(tc.in, from, "192.168.1.50")
		if err != nil || got != tc.want || changed != tc.changed {
			t.Errorf("rewriteHost(%q) = %q, %v, %v; want %q, %v", tc.in, got, changed, err, tc.want, tc.changed)
		}
	}
}

func TestRunApplyAndRestore(t *testing.T) {
	origins := map[string]string{"1": "http://localhost:3001", "2": "http://10.0.0.5:80", "3": "http://127.0.0.1:4000"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/auth/login" && r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case r.URL.Path == "/api/auth/login":
			json.NewEncoder(w).Encode(map[string]string{"token": "tok"})
		case r.Method == http.MethodGet && r.URL.Path == "/api/domains":
			// Two pages to exercise the cursor loop.
			if r.URL.Query().Get("cursor") == "" {
				json.NewEncoder(w).Encode(map[string]any{"items": []domain{{"1", "a.example.com", origins["1"]}, {"2", "b.example.com", origins["2"]}}, "nextCursor": "c1"})
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"items": []domain{{"3", "c.example.com", origins["3"]}}, "nextCursor": ""})
		case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/api/domains/"):
			var body struct {
				OriginURL string `json:"originUrl"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			origins[strings.TrimPrefix(r.URL.Path, "/api/domains/")] = body.OriginURL
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("{}"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("TM_PASSWORD", "pw")

	// Dry run changes nothing.
	if err := run(srv.URL, "admin", "localhost,127.0.0.1", "192.168.1.50", "", false, ""); err != nil {
		t.Fatal(err)
	}
	if origins["1"] != "http://localhost:3001" {
		t.Fatalf("dry run modified origin: %s", origins["1"])
	}

	if err := run(srv.URL, "admin", "localhost,127.0.0.1", "192.168.1.50", "", true, ""); err != nil {
		t.Fatal(err)
	}
	if origins["1"] != "http://192.168.1.50:3001" || origins["3"] != "http://192.168.1.50:4000" || origins["2"] != "http://10.0.0.5:80" {
		t.Fatalf("unexpected origins after apply: %v", origins)
	}

	backups, _ := filepath.Glob(filepath.Join(dir, "origins-backup-*.json"))
	if len(backups) != 1 {
		t.Fatalf("want 1 backup file, got %v", backups)
	}
	if _, err := os.Stat(backups[0]); err != nil {
		t.Fatal(err)
	}

	if err := run(srv.URL, "admin", "", "", "", true, backups[0]); err != nil {
		t.Fatal(err)
	}
	if origins["1"] != "http://localhost:3001" || origins["3"] != "http://127.0.0.1:4000" {
		t.Fatalf("restore did not revert: %v", origins)
	}
}
