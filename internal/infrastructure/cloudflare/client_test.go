package cloudflare

import "testing"

func TestNew_ReturnsClient(t *testing.T) {
	c := New("dummy-token", "dummy-account", "dummy-zone")
	if c == nil {
		t.Fatal("New returned nil")
	}
}
