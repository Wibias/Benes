package storage

import "fmt"

type Error struct {
	Code     string
	Message  string
	TrashDir string
	Count    int
	Bytes    int64
	Partial  bool
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	return e.Code
}

func coded(code, message string) *Error {
	return &Error{Code: code, Message: message}
}

func (e *Error) withTrash(rel string) *Error {
	if e == nil {
		return nil
	}
	e.TrashDir = rel
	return e
}

func (e *Error) withPartial(count int, bytes int64) *Error {
	if e == nil {
		return nil
	}
	e.Partial = true
	e.Count = count
	e.Bytes = bytes
	return e
}

func asError(err error) *Error {
	if err == nil {
		return nil
	}
	if e, ok := err.(*Error); ok {
		return e
	}
	if err == errCodexBusy {
		return coded(CodeCodexBusy, "Codex is using state.sqlite")
	}
	if err == errPathEscape || err == errInvalidRel {
		return coded(CodePathEscape, "path is outside CODEX_HOME")
	}
	return coded(CodeCleanupFailed, fmt.Sprintf("storage operation failed: %s", sanitizeErr(err)))
}

func sanitizeErr(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	if len(msg) > 240 {
		msg = msg[:240]
	}
	return msg
}
