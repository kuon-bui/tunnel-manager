package authcookie

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSetWritesSecureHTTPOnlyStrictCookie(t *testing.T) {
	expiresAt := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	res := httptest.NewRecorder()

	Set(res, "jwt", expiresAt, true)

	cookies := res.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %d", len(cookies))
	}
	cookie := cookies[0]
	if cookie.Name != Name || cookie.Value != "jwt" || cookie.Path != "/api" {
		t.Fatalf("cookie = %#v", cookie)
	}
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("security attributes = %#v", cookie)
	}
	if !cookie.Expires.Equal(expiresAt) || cookie.MaxAge <= 0 {
		t.Fatalf("expiry = %v/%d", cookie.Expires, cookie.MaxAge)
	}
}

func TestClearExpiresCookie(t *testing.T) {
	res := httptest.NewRecorder()

	Clear(res, false)

	cookie := res.Result().Cookies()[0]
	if cookie.Name != Name || cookie.Value != "" || cookie.Path != "/api" {
		t.Fatalf("cookie = %#v", cookie)
	}
	if cookie.MaxAge >= 0 || cookie.Expires.After(time.Now()) || cookie.Secure {
		t.Fatalf("clear attributes = %#v", cookie)
	}
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("security attributes = %#v", cookie)
	}
}
