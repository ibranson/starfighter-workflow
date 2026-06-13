package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"starfighter-workflow/internal/machines"
	"starfighter-workflow/internal/repair"
)

// handleSearchMachines powers the reporting form's type-ahead. ?q= substring-
// matches machine names; empty q returns the first slice alphabetically.
func (s *Server) handleSearchMachines(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	limit := 20
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}
	list, err := s.machines.Search(r.Context(), q, limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"machines": list})
}

// Repair-request routes. All mounted behind requireAuth — any user (or admin)
// may log, view, claim, and drive requests through the workflow.

// handleWorkflowMeta exposes the state-machine catalog so the SPA can render
// the right action for any state without hard-coding the lifecycle. This IS
// the "state machine guidance" surface.
func (s *Server) handleWorkflowMeta(w http.ResponseWriter, _ *http.Request) {
	type st struct {
		Status   repair.Status   `json:"status"`
		Terminal bool            `json:"terminal"`
		Next     []repair.Status `json:"next"`
	}
	out := make([]st, 0, len(repair.AllStatuses))
	for _, s2 := range repair.AllStatuses {
		out = append(out, st{Status: s2, Terminal: repair.IsTerminal(s2), Next: repair.AllowedNext(s2)})
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
	// Machine is the free-text machine name the user typed. It is resolved to
	// a machines-registry row via find-or-create: an existing (case-
	// insensitive) name is reused; a new one is accumulated.
	Machine        string `json:"machine"`
	ProblemSummary string `json:"problem_summary"`
	ProblemDetail  string `json:"problem_detail"`
	Priority       string `json:"priority"`
}

func (s *Server) handleCreateRequest(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r.Context())
	var req createRequestReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	machine, _, err := s.machines.FindOrCreate(r.Context(), req.Machine, u.ID)
	if err != nil {
		if errors.Is(err, machines.ErrEmptyName) {
			writeErr(w, http.StatusBadRequest, "machine is required")
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	rec, err := s.repair.Create(r.Context(), repair.CreateInput{
		MachineID:      machine.ID,
		ProblemSummary: req.ProblemSummary,
		ProblemDetail:  req.ProblemDetail,
		Priority:       req.Priority,
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
	writeJSON(w, http.StatusOK, map[string]any{"request": rec})
}

// handleClaimRequest performs received -> in_repair and makes the caller the
// owner. First-wins: a loser gets 409 + ErrClaimFailed, which the SPA reports.
func (s *Server) handleClaimRequest(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	u := userFromCtx(r.Context())
	rec, err := s.repair.Claim(r.Context(), id, u.ID)
	if err != nil {
		respondRepairErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"request": rec})
}

// handleTakeOverRequest pulls ownership of an owned request to the caller.
func (s *Server) handleTakeOverRequest(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	u := userFromCtx(r.Context())
	rec, err := s.repair.TakeOver(r.Context(), id, u.ID)
	if err != nil {
		respondRepairErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"request": rec})
}

type transitionReq struct {
	Status string `json:"status"`
}

func (s *Server) handleTransitionRequest(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req transitionReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	rec, err := s.repair.Transition(r.Context(), id, repair.Status(req.Status))
	if err != nil {
		respondRepairErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"request": rec})
}

type priorityReq struct {
	Priority string `json:"priority"`
}

func (s *Server) handleSetPriority(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req priorityReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	rec, err := s.repair.SetPriority(r.Context(), id, req.Priority)
	if err != nil {
		respondRepairErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"request": rec})
}

func respondRepairErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, repair.ErrNotFound):
		writeErr(w, http.StatusNotFound, "request not found")
	case errors.Is(err, repair.ErrClaimFailed),
		errors.Is(err, repair.ErrNotOwnable),
		errors.Is(err, repair.ErrUseClaim),
		errors.Is(err, repair.ErrBadTransition):
		writeErr(w, http.StatusConflict, err.Error())
	case errors.Is(err, repair.ErrValidation),
		errors.Is(err, repair.ErrInvalidStatus),
		errors.Is(err, repair.ErrInvalidPriority):
		writeErr(w, http.StatusBadRequest, err.Error())
	default:
		writeErr(w, http.StatusInternalServerError, err.Error())
	}
}
