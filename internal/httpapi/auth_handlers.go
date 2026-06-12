package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"starfighter-workflow/internal/auth"
)

type loginReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type loginResp struct {
	User  *auth.User `json:"user"`
	Token string     `json:"token"` // for non-browser clients
}

type setupReq struct {
	Username    string `json:"username"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
}

// handleSetup creates the very first admin user. Only callable while the users
// table is empty; once any user exists this returns 409.
func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	n, err := s.auth.CountUsers(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if n > 0 {
		writeErr(w, http.StatusConflict, "setup already complete")
		return
	}

	var req setupReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	u, err := s.auth.CreateUser(r.Context(), req.Username, req.Password, auth.RoleAdmin, req.DisplayName)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	// Log the new admin in immediately.
	token, _, err := s.auth.Login(r.Context(), req.Username, req.Password)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	setSessionCookie(w, r, token)
	writeJSON(w, http.StatusCreated, loginResp{User: u, Token: token})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	token, u, err := s.auth.Login(r.Context(), req.Username, req.Password)
	if err != nil {
		if errors.Is(err, auth.ErrInvalidLogin) {
			writeErr(w, http.StatusUnauthorized, "invalid username or password")
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	setSessionCookie(w, r, token)
	writeJSON(w, http.StatusOK, loginResp{User: u, Token: token})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if token := extractToken(r); token != "" {
		_ = s.auth.Logout(r.Context(), token)
	}
	clearSessionCookie(w, r)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"user": userFromCtx(r.Context())})
}

type changePasswordReq struct {
	Password string `json:"password"`
}

// handleChangeMyPassword lets any authenticated user rotate their own
// password. Existing sessions stay valid.
func (s *Server) handleChangeMyPassword(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r.Context())
	if u == nil {
		writeErr(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	var req changePasswordReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := s.auth.ChangePassword(r.Context(), u.ID, req.Password); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
