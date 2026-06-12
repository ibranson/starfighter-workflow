package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"starfighter-workflow/internal/repair"
)

// Repair-request routes. All mounted behind requireAuth — any user (or admin)
// may create, view, and drive requests through the workflow.

// handleWorkflowMeta exposes the state-machine catalog so the SPA can render
// status pickers and transition buttons without hard-coding the (still
// provisional) lifecycle.
func (s *Server) handleWorkflowMeta(w http.ResponseWriter, _ *http.Request) {
	type st struct {
		Status   repair.Status   `json:"status"`
		Terminal bool            `json:"terminal"`
		Next     []repair.Status `json:"next"`
	}
	out := make([]st, 0, len(repair.AllStatuses))
	for _, s2 := range repair.AllStatuses {
		out = append(out, st{
			Status:   s2,
			Terminal: repair.IsTerminal(s2),
			Next:     repair.AllowedNext(s2),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"statuses":   out,
		"priorities": []string{"low", "normal", "high", "urgent"},
	})
}

func (s *Server) handleListRequests(w http.ResponseWriter, r *http.Request) {
	f := repair.ListFilter{
		Status:   r.URL.Query().Get("status"),
		OpenOnly: r.URL.Query().Get("open") == "1",
	}
	reqs, err := s.repair.List(r.Context(), f)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"requests": reqs})
}

type createRequestReq struct {
	GameTitle       string `json:"game_title"`
	CabinetRef      string `json:"cabinet_ref"`
	ProblemSummary  string `json:"problem_summary"`
	ProblemDetail   string `json:"problem_detail"`
	ReporterName    string `json:"reporter_name"`
	ReporterContact string `json:"reporter_contact"`
	Priority        string `json:"priority"`
}

func (s *Server) handleCreateRequest(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r.Context())
	var req createRequestReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	rec, err := s.repair.Create(r.Context(), repair.CreateInput{
		GameTitle:       req.GameTitle,
		CabinetRef:      req.CabinetRef,
		ProblemSummary:  req.ProblemSummary,
		ProblemDetail:   req.ProblemDetail,
		ReporterName:    req.ReporterName,
		ReporterContact: req.ReporterContact,
		Priority:        req.Priority,
	}, u.ID)
	if err != nil {
		respondRepairErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"request": rec})
}

func (s *Server) handleGetRequest(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	rec, err := s.repair.Get(r.Context(), id)
	if err != nil {
		respondRepairErr(w, err)
		return
	}
	events, err := s.repair.Events(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"request": rec, "events": events})
}

type transitionReq struct {
	Status string `json:"status"`
	Note   string `json:"note"`
}

func (s *Server) handleTransitionRequest(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	u := userFromCtx(r.Context())
	var req transitionReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	rec, err := s.repair.Transition(r.Context(), id, repair.Status(req.Status), u.ID, req.Note)
	if err != nil {
		respondRepairErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"request": rec})
}

type assignReq struct {
	AssignedTo *int64 `json:"assigned_to"` // null clears the assignment
	Note       string `json:"note"`
}

func (s *Server) handleAssignRequest(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	u := userFromCtx(r.Context())
	var req assignReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	rec, err := s.repair.Assign(r.Context(), id, req.AssignedTo, u.ID, req.Note)
	if err != nil {
		respondRepairErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"request": rec})
}

type priorityReq struct {
	Priority string `json:"priority"`
	Note     string `json:"note"`
}

func (s *Server) handleSetPriority(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	u := userFromCtx(r.Context())
	var req priorityReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	rec, err := s.repair.SetPriority(r.Context(), id, req.Priority, u.ID, req.Note)
	if err != nil {
		respondRepairErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"request": rec})
}

type noteReq struct {
	Note string `json:"note"`
}

func (s *Server) handleAddNote(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	u := userFromCtx(r.Context())
	var req noteReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	ev, err := s.repair.AddNote(r.Context(), id, u.ID, req.Note)
	if err != nil {
		respondRepairErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"event": ev})
}

func respondRepairErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, repair.ErrNotFound):
		writeErr(w, http.StatusNotFound, "request not found")
	case errors.Is(err, repair.ErrBadTransition):
		writeErr(w, http.StatusConflict, err.Error())
	case errors.Is(err, repair.ErrValidation),
		errors.Is(err, repair.ErrInvalidStatus),
		errors.Is(err, repair.ErrInvalidPriority):
		writeErr(w, http.StatusBadRequest, err.Error())
	default:
		writeErr(w, http.StatusInternalServerError, err.Error())
	}
}
