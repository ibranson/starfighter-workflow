package repair

// ---------------------------------------------------------------------------
// Workflow state machine.
//
// The whole lifecycle lives in this one file. The `status` column is free TEXT
// (no SQL CHECK), so this can evolve with no schema migration.
//
// The shop model (defined with the user 2026-06-12): these are our OWN arcade
// machines, repaired in-place and in-house by a few users working
// independently. There is no customer, no pickup, no approval gate.
//
//   received        reported, UNCLAIMED (no owner)
//   in_repair       claimed/owned and being worked — this single state covers
//                   diagnosing, repairing, and testing (functionally the same)
//   awaiting_parts  a DEVIATION: couldn't be fixed in one attempt; the machine
//                   is offline until further notice
//   completed       fixed, back online — terminal
//   cancelled       closed without repair (no fault found, resolved itself,
//                   user error, machine deprecated/taken offline) — terminal
//
// Ownership:
//   - The received -> in_repair move IS the "claim" and is executed only via
//     the claim path (Service.Claim), which sets the actor as owner. First
//     claim wins; losers are told the request was already claimed. So a plain
//     Transition refuses received -> in_repair (see ErrUseClaim).
//   - Ownership is pull-only: any user may take a request over (become its
//     owner); nobody can delegate/push it to someone else. There is no
//     "release" — taking over is enough to prevent an orphaned request.
// ---------------------------------------------------------------------------

type Status string

const (
	StatusReceived      Status = "received"
	StatusInRepair      Status = "in_repair"
	StatusAwaitingParts Status = "awaiting_parts"
	StatusCompleted     Status = "completed"
	StatusCancelled     Status = "cancelled"
)

// AllStatuses is the ordered catalog the UI uses; terminal states last.
var AllStatuses = []Status{
	StatusReceived,
	StatusInRepair,
	StatusAwaitingParts,
	StatusCompleted,
	StatusCancelled,
}

var terminalStatuses = map[Status]bool{
	StatusCompleted: true,
	StatusCancelled: true,
}

// transitions[from] lists the status moves `from` may make, EXCLUDING the
// universal "cancel from any non-terminal" edge (handled in CanTransition).
// received -> in_repair is the claim edge: it's a real transition, but it is
// executed only through Service.Claim, never a plain Transition.
var transitions = map[Status][]Status{
	StatusReceived:      {StatusInRepair},
	StatusInRepair:      {StatusAwaitingParts, StatusCompleted},
	StatusAwaitingParts: {StatusInRepair, StatusCompleted},
	StatusCompleted:     {}, // terminal
	StatusCancelled:     {}, // terminal
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

// CanTransition reports whether moving from -> to is permitted. Cancelling is
// allowed from any non-terminal state.
func CanTransition(from, to Status) bool {
	if !validStatus(from) || !validStatus(to) || from == to {
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

// AllowedNext returns the statuses reachable from `from` in one step — the
// state-machine guidance the UI renders as action buttons. Note that the
// received -> in_repair entry here is surfaced as a "Claim" action, not a
// plain transition (see the package docs).
func AllowedNext(from Status) []Status {
	out := []Status{}
	for _, to := range AllStatuses {
		if CanTransition(from, to) {
			out = append(out, to)
		}
	}
	return out
}
