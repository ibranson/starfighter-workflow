package repair

// ---------------------------------------------------------------------------
// Workflow state machine — PROVISIONAL.
//
// The user will specify the real repair lifecycle (states + legal transitions)
// later. Everything the rest of the codebase needs to know about the lifecycle
// is funneled through THIS file, so swapping in the final state machine is a
// localized edit:
//
//   - the set of valid statuses           -> AllStatuses / validStatus
//   - which statuses are terminal         -> terminalStatuses
//   - which transitions are legal          -> transitions
//   - the starting status for a new request -> StatusReceived (see repair.go)
//
// No SQL CHECK constraint pins these down (status is a free TEXT column), so
// the final machine can land without a schema migration.
// ---------------------------------------------------------------------------

type Status string

const (
	StatusReceived       Status = "received"         // logged, not yet looked at
	StatusDiagnosing     Status = "diagnosing"       // tech is identifying the fault
	StatusAwaitingParts  Status = "awaiting_parts"   // blocked on ordered parts
	StatusInRepair       Status = "in_repair"        // actively being worked on
	StatusTesting        Status = "testing"          // repaired, under test/burn-in
	StatusReadyForPickup Status = "ready_for_pickup" // done, awaiting return to owner
	StatusOnHold         Status = "on_hold"          // paused (customer/approval/etc.)
	StatusCompleted      Status = "completed"        // terminal: returned/closed-out
	StatusCancelled      Status = "cancelled"        // terminal: abandoned/won't-fix
)

// AllStatuses is the ordered catalog the UI uses to render pickers and the
// board. Order is the natural pipeline order; terminal states last.
var AllStatuses = []Status{
	StatusReceived,
	StatusDiagnosing,
	StatusAwaitingParts,
	StatusInRepair,
	StatusTesting,
	StatusReadyForPickup,
	StatusOnHold,
	StatusCompleted,
	StatusCancelled,
}

var terminalStatuses = map[Status]bool{
	StatusCompleted: true,
	StatusCancelled: true,
}

// transitions[from] lists the statuses `from` may move to. Provisional and
// deliberately permissive within the active pipeline; the final spec will
// tighten this. Any active (non-terminal) status may also be cancelled.
var transitions = map[Status][]Status{
	StatusReceived:       {StatusDiagnosing, StatusOnHold},
	StatusDiagnosing:     {StatusAwaitingParts, StatusInRepair, StatusOnHold},
	StatusAwaitingParts:  {StatusInRepair, StatusOnHold},
	StatusInRepair:       {StatusTesting, StatusAwaitingParts, StatusOnHold},
	StatusTesting:        {StatusReadyForPickup, StatusInRepair, StatusOnHold},
	StatusReadyForPickup: {StatusCompleted, StatusTesting},
	StatusOnHold:         {StatusReceived, StatusDiagnosing, StatusInRepair},
	StatusCompleted:      {}, // terminal
	StatusCancelled:      {}, // terminal
}

func validStatus(s Status) bool {
	for _, v := range AllStatuses {
		if v == s {
			return true
		}
	}
	return false
}

// IsTerminal reports whether s is an end state (no further transitions).
func IsTerminal(s Status) bool { return terminalStatuses[s] }

// CanTransition reports whether moving from -> to is permitted by the current
// (provisional) machine. Cancelling is allowed from any non-terminal state.
func CanTransition(from, to Status) bool {
	if !validStatus(from) || !validStatus(to) {
		return false
	}
	if from == to {
		return false
	}
	if to == StatusCancelled {
		return !IsTerminal(from)
	}
	for _, allowed := range transitions[from] {
		if allowed == to {
			return true
		}
	}
	return false
}

// AllowedNext returns the statuses reachable from `from` in one step, for the
// UI to render only valid transition buttons.
func AllowedNext(from Status) []Status {
	out := []Status{}
	for _, to := range AllStatuses {
		if CanTransition(from, to) {
			out = append(out, to)
		}
	}
	return out
}
