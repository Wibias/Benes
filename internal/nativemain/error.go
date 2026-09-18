package nativemain

import "fmt"

type Error struct {
	Code               string
	Message            string
	Status             int
	Retryable          bool
	CleanupRequired    bool
	PlaintextMayRemain bool
}

func (e *Error) Error() string {
	if e == nil {
		return "native profile error"
	}
	return e.Message
}

func fail(code, message string, status int) *Error {
	return &Error{Code: code, Message: message, Status: status}
}

func AsError(err error) *Error {
	if err == nil {
		return fail("INTERNAL_ERROR", "Native-profile operation failed.", 500)
	}
	if te, ok := err.(*Error); ok {
		if te.Status == 0 {
			te.Status = 500
		}
		return te
	}
	return fail("INTERNAL_ERROR", "Native-profile operation failed.", 500)
}

func isErrno(err error, name string) bool {
	return err != nil && fmt.Sprint(err) != "" && (err.Error() == name || containsCode(err, name))
}

func containsCode(err error, name string) bool {
	type coder interface{ Timeout() bool }
	_ = coder(nil)
	s := err.Error()
	return len(s) > 0 && (s == name || len(name) > 0 && (contains(s, name)))
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
