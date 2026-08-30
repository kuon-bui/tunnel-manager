package cloudflare

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	cloudflareapi "github.com/cloudflare/cloudflare-go/v6"
	"github.com/cloudflare/cloudflare-go/v6/option"
)

func TestListZonesFetchesAllPagesAndReturnsSortedActiveZones(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/zones" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.URL.Query().Get("account.id"); got != "account-1" {
			t.Errorf("account.id = %q", got)
		}
		if got := r.URL.Query().Get("status"); got != "active" {
			t.Errorf("status = %q", got)
		}

		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("page") {
		case "1":
			fmt.Fprint(w, `{"success":true,"errors":[],"messages":[],"result":[{"id":"zone-b","name":"B.EXAMPLE.","status":"active"},{"id":"zone-x","name":"ignored.example","status":"pending"}],"result_info":{"page":1,"per_page":50}}`)
		case "2":
			fmt.Fprint(w, `{"success":true,"errors":[],"messages":[],"result":[{"id":"zone-a","name":"a.example","status":"active"}],"result_info":{"page":2,"per_page":50}}`)
		default:
			fmt.Fprint(w, `{"success":true,"errors":[],"messages":[],"result":[],"result_info":{"page":3,"per_page":50}}`)
		}
	}))
	defer server.Close()

	client := &client{
		api: cloudflareapi.NewClient(
			option.WithBaseURL(server.URL),
			option.WithAPIToken("test-token"),
			option.WithMaxRetries(0),
		),
		accountID: "account-1",
	}

	got, err := client.ListZones(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"zone-a:a.example:active", "zone-b:b.example:active"}
	formatted := make([]string, 0, len(got))
	for _, zone := range got {
		formatted = append(formatted, zone.ID+":"+zone.Name+":"+zone.Status)
	}
	if !reflect.DeepEqual(formatted, want) {
		t.Fatalf("zones = %#v, want %#v", formatted, want)
	}
	if requests != 3 {
		t.Fatalf("requests = %d, want 3", requests)
	}
}

func TestPutIngressConfigWritesOrderedRulesAndCatchAll(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/accounts/account-1/cfd_tunnel/tunnel-1/configurations" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"success":true,"errors":[],"messages":[],"result":{"config":{"ingress":[]},"tunnel_id":"tunnel-1","version":1}}`)
	}))
	defer server.Close()

	client := &client{
		api:       cloudflareapi.NewClient(option.WithBaseURL(server.URL), option.WithAPIToken("test-token"), option.WithMaxRetries(0)),
		accountID: "account-1",
	}
	if err := client.PutIngressConfig(context.Background(), "tunnel-1", "app.example.com", []IngressRule{
		{Path: `^/api(/.*)?$`, Service: "http://127.0.0.1:20080"},
		{Service: "http://localhost:5173"},
	}); err != nil {
		t.Fatal(err)
	}

	configBody := body["config"].(map[string]any)
	ingress := configBody["ingress"].([]any)
	if len(ingress) != 3 {
		t.Fatalf("ingress count = %d", len(ingress))
	}
	first := ingress[0].(map[string]any)
	root := ingress[1].(map[string]any)
	catchAll := ingress[2].(map[string]any)
	if first["hostname"] != "app.example.com" || first["path"] != `^/api(/.*)?$` || first["service"] != "http://127.0.0.1:20080" {
		t.Fatalf("first ingress = %#v", first)
	}
	if _, exists := root["path"]; exists || root["service"] != "http://localhost:5173" {
		t.Fatalf("root ingress = %#v", root)
	}
	if len(catchAll) != 1 || catchAll["service"] != "http_status:404" {
		t.Fatalf("catch-all ingress = %#v", catchAll)
	}
}

func TestListZonesReturnsPaginationError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("page") == "1" {
			fmt.Fprint(w, `{"success":true,"errors":[],"messages":[],"result":[{"id":"zone-1","name":"example.com","status":"active"}],"result_info":{"page":1,"per_page":50}}`)
			return
		}
		w.WriteHeader(http.StatusBadGateway)
		fmt.Fprint(w, `{"success":false,"errors":[{"code":1000,"message":"upstream failed"}],"messages":[],"result":[]}`)
	}))
	defer server.Close()

	client := &client{
		api: cloudflareapi.NewClient(
			option.WithBaseURL(server.URL),
			option.WithAPIToken("test-token"),
			option.WithMaxRetries(0),
		),
		accountID: "account-1",
	}

	if _, err := client.ListZones(context.Background()); err == nil {
		t.Fatal("expected pagination error")
	}
}

func TestDNSMethodsUseExplicitZoneID(t *testing.T) {
	requests := make([]string, 0, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			fmt.Fprint(w, `{"success":true,"errors":[],"messages":[],"result":{"id":"record-1","name":"app.example.com","type":"CNAME","content":"tunnel-1.cfargotunnel.com","ttl":1,"proxied":true}}`)
			return
		}
		fmt.Fprint(w, `{"success":true,"errors":[],"messages":[],"result":{"id":"record-1"}}`)
	}))
	defer server.Close()

	client := &client{api: cloudflareapi.NewClient(option.WithBaseURL(server.URL), option.WithAPIToken("test-token")), accountID: "account-1"}
	recordID, err := client.CreateDNSRecord(context.Background(), "zone-create", "app.example.com", "tunnel-1")
	if err != nil {
		t.Fatal(err)
	}
	if recordID != "record-1" {
		t.Fatalf("record ID = %q", recordID)
	}
	if err := client.DeleteDNSRecord(context.Background(), "zone-delete", "record-1"); err != nil {
		t.Fatal(err)
	}
	want := []string{"POST /zones/zone-create/dns_records", "DELETE /zones/zone-delete/dns_records/record-1"}
	if !reflect.DeepEqual(requests, want) {
		t.Fatalf("requests = %#v, want %#v", requests, want)
	}
}
