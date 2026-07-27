package cloudflare

import (
	"context"
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
			t.Fatalf("path = %q", r.URL.Path)
		}
		if got := r.URL.Query().Get("account.id"); got != "account-1" {
			t.Fatalf("account.id = %q", got)
		}
		if got := r.URL.Query().Get("status"); got != "active" {
			t.Fatalf("status = %q", got)
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
