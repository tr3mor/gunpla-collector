package web

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"net/url"
	"strings"
)

// WithWriteToken requires "Authorization: Bearer <token>" on every request
// that changes data (watches, near-miss review). Reads stay open. With no
// token, writes are accepted from same-origin browser pages and from
// non-browser clients, so set one before exposing the UI beyond a network
// you trust.
func WithWriteToken(token string) Option {
	return func(s *Server) { s.writeToken = token }
}

// write wraps a handler that changes data.
//
// Without a token the defence is against other websites driving the user's
// browser: the body must be JSON (a cross-site form can't send that without
// a CORS preflight, which this server never grants) and a browser-supplied
// Origin must be this server's own.
func (s *Server) write(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := s.checkWrite(r); err != nil {
			status := http.StatusForbidden
			if errors.Is(err, errUnauthorized) {
				status = http.StatusUnauthorized
				w.Header().Set("WWW-Authenticate", `Bearer realm="gunpla-collector"`)
			}
			s.writeError(w, status, err)
			return
		}
		h(w, r)
	}
}

var errUnauthorized = errors.New("a valid write token is required")

func (s *Server) checkWrite(r *http.Request) error {
	if s.writeToken != "" {
		got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if subtle.ConstantTimeCompare([]byte(got), []byte(s.writeToken)) != 1 {
			return errUnauthorized
		}
	}
	if r.Method != http.MethodDelete && !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		return errors.New("Content-Type must be application/json")
	}
	if site := r.Header.Get("Sec-Fetch-Site"); site == "cross-site" {
		return errors.New("cross-site request refused")
	}
	if origin := r.Header.Get("Origin"); origin != "" && s.writeToken == "" {
		u, err := url.Parse(origin)
		if err != nil || u.Host != r.Host {
			return errors.New("cross-origin request refused")
		}
	}
	return nil
}
