package runtimestate

import "testing"

func TestClassifyTreatsParsedFileAsEvidenceNotAuthority(t *testing.T) {
	rec := Record{PID: 4242, Port: 23100, Hostname: "127.0.0.1", Exe: "/usr/bin/benes"}
	dead := Classify(rec, ProcessInfo{}, true)
	if dead.Running() || dead.Class != ClassDead {
		t.Fatalf("dead=%#v", dead)
	}
	reused := Classify(rec, ProcessInfo{Alive: true, Exe: `C:\Windows\System32\ping.exe`}, true)
	if reused.Running() || reused.CanSignal() || reused.Class != ClassReused {
		t.Fatalf("reused=%#v", reused)
	}
	gone := Classify(rec, ProcessInfo{Alive: true, Exe: "/usr/bin/benes"}, false)
	if gone.Running() || gone.Class != ClassNoListener || !gone.CanSignal() {
		t.Fatalf("no listener=%#v", gone)
	}
	live := Classify(rec, ProcessInfo{Alive: true, Exe: "/usr/bin/benes"}, true)
	if !live.Running() || live.Class != ClassLive {
		t.Fatalf("live=%#v", live)
	}
}

func TestClassifyUnknownExeCannotBeSignaled(t *testing.T) {
	rec := Record{PID: 9, Port: 80}
	got := Classify(rec, ProcessInfo{Alive: true, Exe: "/usr/sbin/sshd"}, true)
	if got.CanSignal() || got.Running() {
		t.Fatalf("unrelated=%#v", got)
	}
}

func TestLoopbackProbeHostRejectsNonLoopback(t *testing.T) {
	if _, ok := LoopbackProbeHost("8.8.8.8"); ok {
		t.Fatal("probed public host")
	}
	host, ok := LoopbackProbeHost("0.0.0.0")
	if !ok || host != "127.0.0.1" {
		t.Fatalf("wildcard=%s %v", host, ok)
	}
}
