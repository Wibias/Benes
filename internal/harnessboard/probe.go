package harnessboard

import (
	"time"

	"github.com/Wibias/Benes/internal/codexauth"
)

type Token string

const (
	TokenNone    Token = "none"
	TokenMissing Token = "missing"
	TokenExpired Token = "expired"
	TokenValid   Token = "valid"
)

type Client struct {
	ID         string   `json:"clientId"`
	DetectPath *string  `json:"detectPath"`
	LogPath    *string  `json:"logPath"`
	Running    *bool    `json:"running"`
	Token      Token    `json:"token"`
	Settings   Settings `json:"settings"`
}

type ProbeInput struct {
	BenesHome string
	CodexHome string
	Env       map[string]string
	Now       time.Time
}

func Probe(in ProbeInput) ([]Client, error) {
	settings, err := LoadSettings(in.BenesHome)
	if err != nil {
		return nil, err
	}
	var procs []Process
	var procErr error
	procs, procErr = ListProcesses()
	now := in.Now
	if now.IsZero() {
		now = time.Now()
	}
	codexToken := inspectCodexToken(in.CodexHome, now)
	out := make([]Client, 0, len(IDs))
	for _, id := range IDs {
		detect, logPath := resolvePaths(id, in.CodexHome, in.Env)
		row := Client{
			ID:         id,
			DetectPath: maybeString(detect),
			LogPath:    maybeString(logPath),
			Token:      tokenFor(id, codexToken),
			Settings:   settingsFor(settings, id),
		}
		if procErr != nil {
			row.Running = nil
		} else {
			row.Running = runningFor(id, detect, procs)
		}
		out = append(out, row)
	}
	return out, nil
}

func resolvePaths(id, codexHome string, env map[string]string) (detect, logPath string) {
	if fileClient(id) {
		detect, _ = fileDetect(id, env)
	} else {
		detect = nativeDetect(id, codexHome, env)
	}
	logPath = logPathFor(id, detect, codexHome, env)
	return detect, logPath
}

func tokenFor(id string, codex Token) Token {
	switch id {
	case "claude", "claude-desktop":
		return TokenMissing
	case "codex":
		return codex
	default:
		return TokenNone
	}
}

func inspectCodexToken(codexHome string, now time.Time) Token {
	src, err := codexauth.NewMainCredentialSource(codexHome)
	if err != nil {
		return TokenMissing
	}
	result := src.Read(now)
	if result.Status == codexauth.MainCredentialExpired {
		return TokenExpired
	}
	return TokenMissing
}
