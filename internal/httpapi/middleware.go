package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/url"

	"starfighter-workflow/internal/auth"
)

type ctxKey int

const ctxUserKey ctxKey = iota

// userFromCtx returns the User attached to the request context, or nil.
func userFromCtx(ctx context.Context) *auth.User {
	u, _ := ctx.Value(ctxUserKey).(*auth.User)
	return u
}

// requireAuth wraps a handler so it only runs when a valid session exists.
// This is the floor for every workflow action — a "user" can do everything
// here; only user-management routes additionally require admin.
func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := extractToken(r)
		if token == "" {
			writeErr(w, http.StatusUnauthorized, "not authenticated")
			return
		}
		u, err := s.auth.Lookup(r.Context(), token)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, "session expired")
			return
		}
		ctx := context.WithValue(r.Context(), ctxUserKey, u)
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}

// requireAdmin is requireAuth + an admin role check. Reserved for user
// management — the one thing a plain "user" cannot do.
func (s *Server) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return s.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		u := userFromCtx(r.Context())
		if u == nil || u.Role != auth.RoleAdmin {
			writeErr(w, http.StatusForbidden, "admin only")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requireSameOrigin guards mutating requests against CSRF by verifying the
// Origin header matches the Host. Bearer-token (non-cookie) clients are let
// through since CSRF only applies when ambient cookies drive the request.
func (s *Server) requireSameOrigin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		if _, err := r.Cookie(sessionCookieName); errors.Is(err, http.ErrNoCookie) {
			next.ServeHTTP(w, r)
			return
		}
		origin := r.Header.Get("Origin")
		if origin == "" {
			writeErr(w, http.StatusForbidden, "origin required")
			return
		}
		u, err := url.Parse(origin)
		if err != nil || u.Host != r.Host {
			writeErr(w, http.StatusForbidden, "origin mismatch")
			return
		}
		next.ServeHTTP(w, r)
	}
}
