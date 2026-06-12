// Package httpapi exposes the daemon's HTTP surface.
//
// This is the single source of truth for the embedded SvelteKit SPA and any
// future API client: both use the exact same JSON routes. The mux is built
// once at startup; per-request state lives in handlers.
//
// Authorization model is deliberately simple — two roles:
//   - every authenticated request (requireAuth) may drive the repair workflow;
//   - user management (requireAdmin) is the only admin-gated surface.
package httpapi

import (
	"log/slog"
	"net/http"
	"time"

	"starfighter-workflow/internal/auth"
	"starfighter-workflow/internal/config"
	"starfighter-workflow/internal/db"
	"starfighter-workflow/internal/repair"
	"starfighter-workflow/internal/version"
	"starfighter-workflow/internal/web"
)

type Server struct {
	cfg        config.Config
	configPath string
	logger     *slog.Logger
	db         *db.DB
	auth       *auth.Service
	repair     *repair.Service
	mux        *http.ServeMux
}

func NewServer(
	cfg config.Config, configPath string, logger *slog.Logger, d *db.DB,
	authSvc *auth.Service, repairSvc *repair.Service,
) *Server {
	s := &Server{
		cfg:        cfg,
		configPath: configPath,
		logger:     logger,
		db:         d,
		auth:       authSvc,
		repair:     repairSvc,
		mux:        http.NewServeMux(),
	}
	s.routes()
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /healthz", s.handleHealthz)

	// Public — drives first-boot detection and the login UX.
	s.mux.HandleFunc("GET /api/v1/status", s.handleStatus)
	s.mux.HandleFunc("POST /api/v1/setup", s.requireSameOrigin(s.handleSetup))
	s.mux.HandleFunc("POST /api/v1/auth/login", s.requireSameOrigin(s.handleLogin))

	// Session.
	s.mux.HandleFunc("POST /api/v1/auth/logout", s.requireSameOrigin(s.requireAuth(s.handleLogout)))
	s.mux.HandleFunc("GET /api/v1/auth/me", s.requireAuth(s.handleMe))
	s.mux.HandleFunc("POST /api/v1/auth/me/password", s.requireSameOrigin(s.requireAuth(s.handleChangeMyPassword)))

	// User management — admin only. This is the sole admin/user distinction.
	s.mux.HandleFunc("GET /api/v1/users", s.requireAdmin(s.handleListUsers))
	s.mux.HandleFunc("POST /api/v1/users", s.requireSameOrigin(s.requireAdmin(s.handleCreateUser)))
	s.mux.HandleFunc("DELETE /api/v1/users/{id}", s.requireSameOrigin(s.requireAdmin(s.handleDeleteUser)))
	s.mux.HandleFunc("POST /api/v1/users/{id}/password", s.requireSameOrigin(s.requireAdmin(s.handleChangeUserPassword)))
	s.mux.HandleFunc("PATCH /api/v1/users/{id}", s.requireSameOrigin(s.requireAdmin(s.handleChangeUserRole)))

	// Workflow metadata (state-machine catalog for the SPA).
	s.mux.HandleFunc("GET /api/v1/workflow/meta", s.requireAuth(s.handleWorkflowMeta))

	// Repair requests — full surface open to any authenticated user.
	s.mux.HandleFunc("GET /api/v1/requests", s.requireAuth(s.handleListRequests))
	s.mux.HandleFunc("POST /api/v1/requests", s.requireSameOrigin(s.requireAuth(s.handleCreateRequest)))
	s.mux.HandleFunc("GET /api/v1/requests/{id}", s.requireAuth(s.handleGetRequest))
	s.mux.HandleFunc("POST /api/v1/requests/{id}/transition", s.requireSameOrigin(s.requireAuth(s.handleTransitionRequest)))
	s.mux.HandleFunc("POST /api/v1/requests/{id}/assign", s.requireSameOrigin(s.requireAuth(s.handleAssignRequest)))
	s.mux.HandleFunc("POST /api/v1/requests/{id}/priority", s.requireSameOrigin(s.requireAuth(s.handleSetPriority)))
	s.mux.HandleFunc("POST /api/v1/requests/{id}/notes", s.requireSameOrigin(s.requireAuth(s.handleAddNote)))

	// SPA fallback for everything else; SvelteKit handles client-side routing.
	s.mux.Handle("/", web.Handler())
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok\n"))
}

// handleStatus is unauthenticated; the login screen reads it to decide whether
// to show the first-boot setup wizard.
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	users, err := s.auth.CountUsers(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"display_name": s.cfg.DisplayName,
		"needs_setup":  users == 0,
		"version":      version.Current,
		"server_time":  time.Now().Format(time.RFC3339),
	})
}
