package domainservice

import (
	"errors"
	"regexp"
	"testing"
	"time"

	domainrequest "tunnelmanager/internal/pkg/request/domain"
)

func TestNormalizeRoutesOrdersSpecificPrefixesAndRoot(t *testing.T) {
	routes, defaultOrigin, err := normalizeRoutes("domain-1", []domainrequest.RouteInput{
		{Path: "/", OriginURL: "http://localhost:5173/"},
		{Path: " /api/ ", OriginURL: "http://localhost:8080", StripPrefix: true},
		{Path: "/api/v2", OriginURL: "http://localhost:8081", StripPrefix: true},
	}, time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	if defaultOrigin != "http://localhost:5173" {
		t.Fatalf("default origin = %q", defaultOrigin)
	}
	wantPaths := []string{"/api/v2", "/api", "/"}
	for i, want := range wantPaths {
		if routes[i].Path != want {
			t.Fatalf("route %d path = %q, want %q", i, routes[i].Path, want)
		}
	}
}

func TestNormalizeRoutesRejectsInvalidSets(t *testing.T) {
	tests := []struct {
		name   string
		routes []domainrequest.RouteInput
	}{
		{name: "missing root", routes: []domainrequest.RouteInput{{Path: "/api", OriginURL: "http://localhost:8080"}}},
		{name: "duplicate normalized path", routes: []domainrequest.RouteInput{{Path: "/api", OriginURL: "http://localhost:8080"}, {Path: "/api/", OriginURL: "http://localhost:8081"}, {Path: "/", OriginURL: "http://localhost:3000"}}},
		{name: "strip root", routes: []domainrequest.RouteInput{{Path: "/", OriginURL: "http://localhost:8080", StripPrefix: true}}},
		{name: "query in path", routes: []domainrequest.RouteInput{{Path: "/api?x=1", OriginURL: "http://localhost:8080"}, {Path: "/", OriginURL: "http://localhost:3000"}}},
		{name: "origin path", routes: []domainrequest.RouteInput{{Path: "/", OriginURL: "http://localhost:8080/base"}}},
		{name: "unsupported origin", routes: []domainrequest.RouteInput{{Path: "/", OriginURL: "tcp://localhost:8080"}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := normalizeRoutes("domain-1", test.routes, time.Now())
			if !errors.Is(err, ErrInvalidRoutes) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestCloudflareRulesUseBoundaryRegexAndProxyForStripping(t *testing.T) {
	service := &domainService{proxy: &fakeIngressProxy{}}
	routes, _, err := normalizeRoutes("domain-1", []domainrequest.RouteInput{
		{Path: "/api", OriginURL: "http://localhost:8080", StripPrefix: true},
		{Path: "/", OriginURL: "http://localhost:5173"},
	}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	rules := service.cloudflareRules(routes)
	if rules[0].Path != `^/api(/.*)?$` || rules[0].Service != "http://127.0.0.1:20080" {
		t.Fatalf("api rule = %#v", rules[0])
	}
	compiled, err := regexp.Compile(rules[0].Path)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api", "/api/users"} {
		if !compiled.MatchString(path) {
			t.Fatalf("regex did not match %q", path)
		}
	}
	if compiled.MatchString("/apiary") {
		t.Fatal("regex matched /apiary")
	}
	if rules[1].Path != "" || rules[1].Service != "http://localhost:5173" {
		t.Fatalf("root rule = %#v", rules[1])
	}
}
