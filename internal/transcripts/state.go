package transcripts

// State is how alive a session is. Only watchtower records a status of its own;
// for the others the state is inferred.
type State string

const (
	// StateLive means an agent looks attached to this session right now.
	StateLive State = "live"
	// StateActive is a watchtower session that has not been archived.
	StateActive State = "active"
	// StateIdle is a finished conversation you can still resume.
	StateIdle State = "idle"
	// StateArchived is a watchtower session that has been put away.
	StateArchived State = "archived"
)

// States is every state a session can be in.
var States = []State{StateLive, StateActive, StateIdle, StateArchived}

// StateOf combines a session's recorded status with a liveness observation. Pure,
// so the UI can hold liveness separately and never mutate a Session.
func StateOf(s *Session, live bool) State {
	switch {
	case live:
		return StateLive
	case s.Status == "archived":
		return StateArchived
	case s.Status == "active":
		return StateActive
	default:
		return StateIdle
	}
}
