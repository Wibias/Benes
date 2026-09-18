package main

import (
	"encoding/base64"
	"encoding/json"
	"testing"
)

func TestParseTrayHostEntry(t *testing.T) {
	raw, _ := json.Marshal(trayHostEntry{
		CLI: "C:\\benes.exe", Script: "C:\\tray.ps1",
		CodexHome: "C:\\codex", BenesHome: "C:\\benes",
	})
	entry, err := parseTrayHostEntry(base64.StdEncoding.EncodeToString(raw))
	if err != nil || entry.Script != "C:\\tray.ps1" {
		t.Fatalf("entry=%#v err=%v", entry, err)
	}
}

func TestParseTrayHostEntryMissing(t *testing.T) {
	if _, err := parseTrayHostEntry(""); err == nil {
		t.Fatal("expected error")
	}
}

func TestParseTrayHostEntryIgnoresUnknownFields(t *testing.T) {
	raw, _ := json.Marshal(map[string]string{
		"cli": "C:\\benes.exe", "script": "C:\\tray.ps1",
		"codexHome": "C:\\codex", "benesHome": "C:\\benes",
		"extra": "ignored",
	})
	entry, err := parseTrayHostEntry(base64.StdEncoding.EncodeToString(raw))
	if err != nil {
		t.Fatal(err)
	}
	if entry.CLI != "C:\\benes.exe" {
		t.Fatalf("cli=%q", entry.CLI)
	}
}
