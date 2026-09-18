package fabric

import "errors"

const (
	CodeInvalidTask       = "invalid_task"
	CodeInvalidTransition = "invalid_transition"
	CodeCapacityExceeded  = "capacity_exceeded"
)

// Error is a structured kernel error safe to map onto the management API.
type Error struct {
	Code string
	Msg  string
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return e.Msg
}

func InvalidTask(msg string) error {
	return &Error{Code: CodeInvalidTask, Msg: msg}
}

func InvalidTransition(msg string) error {
	return &Error{Code: CodeInvalidTransition, Msg: msg}
}

func CapacityExceeded(msg string) error {
	return &Error{Code: CodeCapacityExceeded, Msg: msg}
}

func CodeOf(err error) string {
	var fe *Error
	if errors.As(err, &fe) {
		return fe.Code
	}
	return ""
}
