package serve

import (
	"crypto/subtle"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// authMethod is how a request proved itself.
type authMethod string

const (
	authNone   authMethod = ""
	authCookie authMethod = "cookie"
	authToken  authMethod = "token"
)

// authenticate inspects a request's bearer token or session cookie.
func (s *Server) authenticate(r *http.Request) authMethod {
	if h := r.Header.Get("Authorization"); h != "" {
		if tok, ok := strings.CutPrefix(h, "Bearer "); ok && s.opts.Token != "" {
			if subtle.ConstantTimeCompare([]byte(tok), []byte(s.opts.Token)) == 1 {
				return authToken
			}
		}
		return authNone
	}
	if c, err := r.Cookie(sessionCookie); err == nil {
		if s.codec.verify(c.Value, time.Now()) {
			return authCookie
		}
	}
	return authNone
}

// authed guards an endpoint: valid bearer token or session cookie required.
// Cookie-authenticated mutations additionally pass a same-origin (CSRF)
// check; token requests carry no ambient credentials and are exempt (W7).
func (s *Server) authed(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method := s.authenticate(r)
		if method == authNone {
			writeErr(w, http.StatusUnauthorized, "unauthorized", "authentication required")
			return
		}
		if method == authCookie && isMutation(r.Method) && !sameOrigin(r) {
			writeErr(w, http.StatusForbidden, "cross_origin", "cross-origin request rejected")
			return
		}
		next(w, r)
	})
}

func isMutation(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	}
	return true
}

// sameOrigin reports whether a browser request came from this UI's own
// origin. Browsers always send Origin and/or Sec-Fetch-Site on cross-origin
// requests; a request carrying neither did not come from a browser page and
// is rejected for cookie-authenticated mutations.
func sameOrigin(r *http.Request) bool {
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		if err != nil {
			return false
		}
		return strings.EqualFold(u.Host, r.Host)
	}
	switch r.Header.Get("Sec-Fetch-Site") {
	case "same-origin", "none":
		return true
	}
	return false
}

// loginOriginOK applies the same-origin check to login endpoints when an
// Origin header is present (login CSRF); header-less clients (curl) pass.
func loginOriginOK(r *http.Request) bool {
	if r.Header.Get("Origin") == "" && r.Header.Get("Sec-Fetch-Site") == "" {
		return true
	}
	return sameOrigin(r)
}

// setSessionCookie mints a fresh session for the client. Secure is set when
// the request arrived over HTTPS (directly or via the tailnet proxy).
func (s *Server) setSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    s.codec.mint(time.Now()),
		Path:     "/",
		MaxAge:   int(sessionTTL / time.Second),
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   requestIsHTTPS(r),
	})
}

func requestIsHTTPS(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if !loginOriginOK(r) {
		writeErr(w, http.StatusForbidden, "cross_origin", "cross-origin request rejected")
		return
	}
	if s.opts.Password == "" {
		writeErr(w, http.StatusForbidden, "password_disabled", "no password is configured; use a bearer token")
		return
	}
	var body struct {
		Password string `json:"password"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	if subtle.ConstantTimeCompare([]byte(body.Password), []byte(s.opts.Password)) != 1 {
		// Single-user surface: a flat delay is enough brute-force friction.
		time.Sleep(500 * time.Millisecond)
		s.log.Warn("failed password login", "tailscale_user", r.Header.Get("Tailscale-User-Login"))
		writeErr(w, http.StatusUnauthorized, "bad_password", "wrong password")
		return
	}
	s.setSessionCookie(w, r)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	// Logout needs no auth (an expired session should still clear its
	// cookie), but a cross-origin page must not be able to log the phone
	// out.
	if !loginOriginOK(r) {
		writeErr(w, http.StatusForbidden, "cross_origin", "cross-origin request rejected")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   requestIsHTTPS(r),
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleAuthState tells the login screen what it can offer before any
// authentication has happened.
func (s *Server) handleAuthState(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"password_enabled": s.opts.Password != "",
		"has_passkeys":     len(s.store.Credentials()) > 0,
	})
}

// handleMe reports whether (and how) the caller is authenticated.
func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	method := s.authenticate(r)
	if method == authNone {
		writeErr(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"authenticated": true, "method": string(method)})
}
