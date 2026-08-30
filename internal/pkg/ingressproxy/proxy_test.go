package ingressproxy

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"tunnelmanager/internal/model"
	"tunnelmanager/internal/pkg/config"
)

func TestProxyStripsMatchedPrefixAndPreservesRequest(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s|%s|%s|%s|%s|%s", r.URL.Path, r.URL.RawQuery, r.Host, r.Header.Get("X-Forwarded-Prefix"), r.Header.Get("X-Forwarded-Host"), r.Header.Get("X-Forwarded-Proto"))
	}))
	defer origin.Close()

	proxy := New(config.Config{IngressProxyAddr: "127.0.0.1:20080"}).(*proxyServer)
	routes := []model.DomainRoute{{Path: "/api", OriginURL: origin.URL, StripPrefix: true}}
	if err := proxy.CommitDomain("domain-1", "APP.EXAMPLE.COM.", routes); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodGet, "http://app.example.com/api/users?id=1", nil)
	request.Host = "app.example.com:443"
	request.Header.Set("X-Forwarded-Proto", "https")
	response := httptest.NewRecorder()
	proxy.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}
	want := "/users|id=1|app.example.com:443|/api|app.example.com:443|https"
	if response.Body.String() != want {
		t.Fatalf("body = %q, want %q", response.Body.String(), want)
	}

	boundary := httptest.NewRequest(http.MethodGet, "http://app.example.com/apiary", nil)
	boundaryResponse := httptest.NewRecorder()
	proxy.ServeHTTP(boundaryResponse, boundary)
	if boundaryResponse.Code != http.StatusNotFound {
		t.Fatalf("boundary status = %d", boundaryResponse.Code)
	}
}

func TestProxyRejectsLoopbackAliasesAtItsOwnPort(t *testing.T) {
	proxy := New(config.Config{IngressProxyAddr: "127.0.0.1:20080"}).(*proxyServer)
	for _, origin := range []string{"http://localhost:20080", "http://127.0.0.1:20080", "http://[::1]:20080"} {
		err := proxy.CommitDomain("domain-1", "app.example.com", []model.DomainRoute{{Path: "/api", OriginURL: origin, StripPrefix: true}})
		if err == nil {
			t.Fatalf("origin %q did not report a self-loop", origin)
		}
	}
}

func TestProxyRootAfterExactPrefix(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, r.URL.Path)
	}))
	defer origin.Close()

	proxy := New(config.Config{IngressProxyAddr: "127.0.0.1:20080"}).(*proxyServer)
	if err := proxy.CommitDomain("domain-1", "app.example.com", []model.DomainRoute{{Path: "/api", OriginURL: origin.URL, StripPrefix: true}}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api", "/api/"} {
		request := httptest.NewRequest(http.MethodGet, "http://app.example.com"+path, nil)
		response := httptest.NewRecorder()
		proxy.ServeHTTP(response, request)
		if response.Body.String() != "/" {
			t.Fatalf("%s forwarded as %q", path, response.Body.String())
		}
	}
}

func TestPrepareDomainKeepsOldOnlyRoutesUntilCommit(t *testing.T) {
	oldOrigin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "old")
	}))
	defer oldOrigin.Close()
	newOrigin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "new")
	}))
	defer newOrigin.Close()

	proxy := New(config.Config{IngressProxyAddr: "127.0.0.1:20080"}).(*proxyServer)
	oldRoutes := []model.DomainRoute{
		{Path: "/api", OriginURL: oldOrigin.URL, StripPrefix: true},
		{Path: "/legacy", OriginURL: oldOrigin.URL, StripPrefix: true},
	}
	newRoutes := []model.DomainRoute{{Path: "/api", OriginURL: newOrigin.URL, StripPrefix: true}}
	if err := proxy.PrepareDomain("domain-1", "app.example.com", oldRoutes, newRoutes); err != nil {
		t.Fatal(err)
	}
	assertProxyBody(t, proxy, "/api/users", "new")
	assertProxyBody(t, proxy, "/legacy/users", "old")

	if err := proxy.CommitDomain("domain-1", "app.example.com", newRoutes); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "http://app.example.com/legacy/users", nil)
	response := httptest.NewRecorder()
	proxy.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("committed old route status = %d", response.Code)
	}
}

func assertProxyBody(t *testing.T, proxy http.Handler, path, want string) {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "http://app.example.com"+path, nil)
	response := httptest.NewRecorder()
	proxy.ServeHTTP(response, request)
	if response.Body.String() != want {
		t.Fatalf("%s body = %q, want %q", path, response.Body.String(), want)
	}
}
