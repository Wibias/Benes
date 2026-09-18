package runtimestate

import (
	"net"
	"path/filepath"
	"strings"
)

type Record struct {
	PID      int
	Port     int
	Hostname string
	Exe      string
}

type ProcessInfo struct {
	Alive bool
	Exe   string
}

type Class string

const (
	ClassMissing    Class = "missing"
	ClassMalformed  Class = "malformed"
	ClassDead       Class = "dead"
	ClassReused     Class = "reused"
	ClassNoListener Class = "no_listener"
	ClassLive       Class = "live"
)

type Verdict struct {
	Class   Class
	Record  Record
	Process ProcessInfo
}

func (v Verdict) Running() bool { return v.Class == ClassLive }
func (v Verdict) CanSignal() bool {
	return v.Class == ClassLive || v.Class == ClassNoListener
}
func (v Verdict) CanClear() bool {
	return v.Class == ClassDead || v.Class == ClassMalformed
}

func Classify(rec Record, proc ProcessInfo, listenerReady bool) Verdict {
	v := Verdict{Record: rec, Process: proc}
	if rec.PID <= 0 || rec.Port <= 0 || rec.Port > 65535 {
		v.Class = ClassMalformed
		if rec.PID == 0 && rec.Port == 0 && rec.Hostname == "" && rec.Exe == "" {
			v.Class = ClassMissing
		}
		return v
	}
	if !proc.Alive {
		v.Class = ClassDead
		return v
	}
	if !identityMatches(rec.Exe, proc.Exe) {
		v.Class = ClassReused
		return v
	}
	if !listenerReady {
		v.Class = ClassNoListener
		return v
	}
	v.Class = ClassLive
	return v
}

func identityMatches(recorded, live string) bool {
	recorded = strings.TrimSpace(recorded)
	live = strings.TrimSpace(live)
	if recorded == "" && live == "" {
		return false
	}
	if recorded != "" && live != "" {
		if sameExe(recorded, live) {
			return true
		}
		if strings.EqualFold(filepath.Base(recorded), filepath.Base(live)) {
			return true
		}
		return looksLikeBenes(recorded) && looksLikeBenes(live)
	}
	if live != "" {
		return looksLikeBenes(live)
	}
	return looksLikeBenes(recorded)
}

func sameExe(a, b string) bool {
	left, err1 := filepath.EvalSymlinks(a)
	right, err2 := filepath.EvalSymlinks(b)
	if err1 != nil {
		left = a
	}
	if err2 != nil {
		right = b
	}
	return strings.EqualFold(filepath.Clean(left), filepath.Clean(right))
}

func looksLikeBenes(exe string) bool {
	base := strings.ToLower(filepath.Base(strings.TrimSpace(exe)))
	base = strings.TrimSuffix(base, ".exe")
	return base == "benes" || strings.HasPrefix(base, "benes.") || strings.Contains(base, "benes.test")
}

func LoopbackProbeHost(hostname string) (string, bool) {
	host := strings.TrimSpace(hostname)
	if host == "" || host == "0.0.0.0" || host == "::" || host == "[::]" {
		return "127.0.0.1", true
	}
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return host, true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	if ip != nil && ip.IsLoopback() {
		return host, true
	}
	return "", false
}
