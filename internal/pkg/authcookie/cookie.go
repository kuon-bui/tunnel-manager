package authcookie

import (
	"net/http"
	"time"
)

const Name = "tunnel_manager_token"

func Set(w http.ResponseWriter, token string, expiresAt time.Time, secure bool) {
	remaining := time.Until(expiresAt)
	if remaining <= 0 {
		Clear(w, secure)
		return
	}
	maxAge := int(remaining.Seconds())
	if maxAge < 1 {
		maxAge = 1
	}
	http.SetCookie(w, &http.Cookie{
		Name:     Name,
		Value:    token,
		Path:     "/api",
		Expires:  expiresAt,
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
	})
}

func Clear(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     Name,
		Path:     "/api",
		Expires:  time.Unix(1, 0).UTC(),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
	})
}
