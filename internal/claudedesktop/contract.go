package claudedesktop

import (
	"errors"
	"strings"
)

// State is the canonical disposition of the Claude Desktop runtime contract.
//
// It exists instead of a set of booleans because the previous surface let saved
// Benes state stand in for applied native state. A single value forces the
// caller to answer which of these is actually true.
type State string

const (
	// StateUnsupportedHost means Benes may not manage this host at all.
	StateUnsupportedHost State = "unsupported_host"
	// StateNotInstalled means the host is supported but Claude Desktop is not
	// present.
	StateNotInstalled State = "not_installed"
	// StateConfigUnavailable means Claude Desktop is installed but its native
	// configuration cannot be read, parsed, or safely rewritten.
	StateConfigUnavailable State = "config_unavailable"
	// StateNoManagedProjection means the native configuration is manageable but
	// Benes ships no applicable projection, so nothing can be applied.
	StateNoManagedProjection State = "no_managed_projection"
	// StateNotApplied means a desired projection exists and is not natively
	// present.
	StateNotApplied State = "not_applied"
	// StateApplied means the native projection matches the desired projection.
	StateApplied State = "applied"
	// StateStale means a Benes-owned native entry is present but does not match
	// the desired projection.
	StateStale State = "stale"
)

// RefusalCode is the machine-readable reason the runtime cannot proceed.
type RefusalCode string

const (
	// RefusalUnsupportedHost reports an unsupported operating system.
	RefusalUnsupportedHost RefusalCode = "unsupported_host"
	// RefusalNotInstalled reports a supported host with no Claude Desktop.
	RefusalNotInstalled RefusalCode = "not_installed"
	// RefusalConfigUnreadable reports a configuration file that cannot be read.
	RefusalConfigUnreadable RefusalCode = "config_unreadable"
	// RefusalConfigMalformed reports a configuration file that is not a JSON
	// object.
	RefusalConfigMalformed RefusalCode = "config_malformed"
	// RefusalConfigUnmanageable reports a configuration path Benes refuses to
	// rewrite, such as a directory or a symlink.
	RefusalConfigUnmanageable RefusalCode = "config_unmanageable"
	// RefusalRuntimeUnavailable reports that Benes ships no managed projection
	// to install.
	RefusalRuntimeUnavailable RefusalCode = "benes_mcp_runtime_unavailable"
	// RefusalProjectionInvalid reports a desired projection Benes refuses to
	// serialize.
	RefusalProjectionInvalid RefusalCode = "managed_projection_invalid"
	// RefusalDesiredDisabled reports an apply or re-apply while the desired
	// state is disabled.
	RefusalDesiredDisabled RefusalCode = "desired_disabled"
)

// Refusal reports an actionable condition backed by a real runtime signal. It
// carries no synthetic health, timestamp, or guessed state.
type Refusal struct {
	Code    RefusalCode `json:"code"`
	Message string      `json:"message"`
}

// Input is the evidence the contract evaluates.
type Input struct {
	// Host is the operating system to evaluate, for example runtime.GOOS. An
	// empty value means the running host.
	Host string
	// Home is the user home directory. An empty value means the running user's
	// home, so tests and callers can evaluate against an isolated home.
	Home string
	// Env supplies environment values for path resolution. A nil map means the
	// process environment.
	Env map[string]string
	// LookPath resolves an executable name. A nil value means exec.LookPath.
	LookPath func(string) (string, error)
	// DesiredEnabled is Benes' desired enablement for this harness, read from
	// the Benes configuration. It is desired product state and never evidence
	// of applied native state.
	DesiredEnabled bool
	// Managed is the native projection Benes would install, or nil when Benes
	// ships no applicable managed projection.
	Managed *Projection
}

// Status is the single canonical Claude Desktop runtime response.
//
// Every applied field is derived from the native document. Nothing here is
// inferred from a Benes preference, a successful Benes write, a previous API
// response, or process detection.
type Status struct {
	ClientID                   string `json:"clientId"`
	HostSupported              bool   `json:"hostSupported"`
	Installed                  bool   `json:"installed"`
	Configurable               bool   `json:"configurable"`
	ConfigPath                 string `json:"configPath,omitempty"`
	State                      State  `json:"state"`
	ManagedProjectionAvailable bool   `json:"managedProjectionAvailable"`
	DesiredEnabled             bool   `json:"desiredEnabled"`
	// DesiredFingerprint identifies the desired managed projection, and is
	// omitted when no desired projection exists.
	DesiredFingerprint string `json:"desiredFingerprint,omitempty"`
	// ObservedKind is what the native document actually holds at the managed
	// key path.
	ObservedKind ObservedKind `json:"observedKind"`
	// Applied is true only when the native projection matches the desired
	// projection.
	Applied bool `json:"applied"`
	// AppliedFingerprint identifies the applied projection, and is present only
	// when Applied is true.
	AppliedFingerprint string `json:"appliedFingerprint,omitempty"`
	// ObservedFingerprint identifies a usable Benes-owned entry found on disk,
	// and is present even when that entry is stale.
	ObservedFingerprint string `json:"observedFingerprint,omitempty"`
	// Stale is true only when a Benes-owned native entry exists and differs from
	// the desired projection.
	Stale bool `json:"stale"`
	// RestartRequired is true when a native mutation is awaiting a Claude
	// Desktop restart. Benes cannot observe that a running instance consumed a
	// changed file, so this stays false until a real mutation is recorded.
	RestartRequired bool `json:"restartRequired"`
	// Refusal is present only when a real runtime signal prevents progress.
	Refusal *Refusal `json:"refusal"`
}

// Evaluate derives the canonical status from real host and native evidence.
func Evaluate(in Input) Status {
	host := normalizeHost(in.Host)
	status := Status{
		ClientID:       ClientID,
		HostSupported:  HostSupported(host),
		DesiredEnabled: in.DesiredEnabled,
		ObservedKind:   ObservedUnobserved,
	}
	if !status.HostSupported {
		status.State = StateUnsupportedHost
		status.Refusal = &Refusal{
			Code:    RefusalUnsupportedHost,
			Message: "Benes manages Claude Desktop native configuration on Windows and macOS only.",
		}
		return status
	}
	home := in.Home
	if strings.TrimSpace(home) == "" {
		home = userHome()
	}
	configPath, err := ConfigPath(host, home, in.Env)
	if err != nil {
		status.HostSupported = false
		status.State = StateUnsupportedHost
		status.Refusal = &Refusal{Code: RefusalUnsupportedHost, Message: err.Error()}
		return status
	}
	status.ConfigPath = configPath
	status.Installed = DetectInstall(host, home, in.Env, in.LookPath) != ""
	if !status.Installed {
		status.State = StateNotInstalled
		status.Refusal = &Refusal{
			Code:    RefusalNotInstalled,
			Message: "Claude Desktop is not installed on this host.",
		}
		return status
	}
	observed, refusal := observeNative(configPath)
	status.ObservedKind = observed.Kind
	if refusal != nil {
		status.State = StateConfigUnavailable
		status.Refusal = refusal
		return status
	}
	status.Configurable = true
	status.ManagedProjectionAvailable = in.Managed != nil
	if in.Managed == nil {
		status.State = StateNoManagedProjection
		status.Refusal = &Refusal{
			Code:    RefusalRuntimeUnavailable,
			Message: RuntimeUnavailableMessage,
		}
		return status
	}
	desired := DesiredProjection(in)
	if desired != nil {
		status.DesiredFingerprint = desired.Fingerprint()
	}
	if observed.Entry != nil {
		status.ObservedFingerprint = observed.Entry.Fingerprint()
	}
	if desired != nil && observed.Kind == ObservedEntryPresent && observed.Entry != nil && observed.Entry.Equal(*desired) {
		status.Applied = true
		status.AppliedFingerprint = desired.Fingerprint()
		status.State = StateApplied
		return status
	}
	if observed.Kind == ObservedEntryPresent || observed.Kind == ObservedEntryUnusable {
		// A Benes-owned entry exists that is not the desired projection: either
		// a different projection, or a shape Benes does not own.
		status.Stale = true
		status.State = StateStale
		return status
	}
	status.State = StateNotApplied
	return status
}

// observeNative reads the native document and reports the Benes-owned state, or
// the refusal that prevents reading it.
func observeNative(path string) (Observed, *Refusal) {
	native, err := ReadNative(path)
	switch {
	case errors.Is(err, ErrNativeMalformed):
		return Observed{Kind: ObservedConfigUnparsable}, &Refusal{
			Code:    RefusalConfigMalformed,
			Message: "Claude Desktop configuration is not a JSON object, so Benes left it untouched.",
		}
	case errors.Is(err, ErrNativeNotRegular):
		return Observed{Kind: ObservedUnobserved}, &Refusal{
			Code:    RefusalConfigUnmanageable,
			Message: "Claude Desktop configuration is not a regular file, so Benes left it untouched.",
		}
	case err != nil:
		return Observed{Kind: ObservedUnobserved}, &Refusal{
			Code:    RefusalConfigUnreadable,
			Message: "Claude Desktop configuration could not be read.",
		}
	case !native.Exists():
		return Observed{Kind: ObservedConfigAbsent}, nil
	default:
		return native.Observe(), nil
	}
}

// mutable reports whether a native mutation may be attempted for a state.
func mutable(state State) bool {
	switch state {
	case StateUnsupportedHost, StateNotInstalled, StateConfigUnavailable:
		return false
	default:
		return true
	}
}

// Apply installs the desired managed projection and returns the truth about
// what happened.
//
// With the production projection source this always refuses, because Benes
// ships no stdio MCP runtime to bind into mcpServers.benes. It never writes a
// placeholder entry and never reports success it did not verify.
func Apply(in Input) (MutationResult, *Refusal) {
	status := Evaluate(in)
	if !mutable(status.State) || status.Refusal != nil {
		result := MutationResult{ClientID: ClientID, ConfigPath: status.ConfigPath}
		if status.Refusal != nil {
			return result, status.Refusal
		}
		return result, &Refusal{Code: RefusalConfigUnmanageable, Message: "Claude Desktop native configuration cannot be changed."}
	}
	desired := DesiredProjection(in)
	if desired == nil {
		return MutationResult{ClientID: ClientID, ConfigPath: status.ConfigPath}, &Refusal{
			Code:    RefusalDesiredDisabled,
			Message: "Claude Desktop managed projection is not desired, so nothing was applied.",
		}
	}
	result, err := InstallNative(status.ConfigPath, *desired)
	if err != nil {
		return MutationResult{ClientID: ClientID, ConfigPath: status.ConfigPath}, writeRefusal(err)
	}
	return result, nil
}

// Disable removes the Benes-owned native entry when it is exactly the
// projection Benes wrote, and reports no change when there is nothing to
// remove. It is idempotent.
func Disable(in Input) (MutationResult, *Refusal) {
	status := Evaluate(in)
	if !mutable(status.State) {
		result := MutationResult{ClientID: ClientID, ConfigPath: status.ConfigPath}
		if status.Refusal != nil {
			return result, status.Refusal
		}
		return result, &Refusal{Code: RefusalConfigUnmanageable, Message: "Claude Desktop native configuration cannot be changed."}
	}
	if in.Managed == nil {
		// Benes owns no projection, so it cannot prove an entry is its own and
		// must not remove one. The truthful result is an unchanged no-op.
		return MutationResult{ClientID: ClientID, ConfigPath: status.ConfigPath}, nil
	}
	result, err := RemoveNative(status.ConfigPath, *in.Managed)
	if err != nil {
		return MutationResult{ClientID: ClientID, ConfigPath: status.ConfigPath}, writeRefusal(err)
	}
	return result, nil
}

// writeRefusal maps a native write failure to its machine-readable refusal.
func writeRefusal(err error) *Refusal {
	switch {
	case errors.Is(err, ErrNativeMalformed):
		return &Refusal{Code: RefusalConfigMalformed, Message: "Claude Desktop configuration is not a JSON object, so Benes left it untouched."}
	case errors.Is(err, ErrNativeNotRegular):
		return &Refusal{Code: RefusalConfigUnmanageable, Message: "Claude Desktop configuration is not a regular file, so Benes left it untouched."}
	case errors.Is(err, ErrNativeBlocked):
		return &Refusal{Code: RefusalConfigUnmanageable, Message: "Claude Desktop configuration holds a non-object mcpServers value that Benes will not replace."}
	case errors.Is(err, ErrForeignEntry):
		return &Refusal{Code: RefusalConfigUnmanageable, Message: "mcpServers.benes is not a Benes-owned projection, so Benes left it untouched."}
	case errors.Is(err, ErrInvalidProjection):
		return &Refusal{Code: RefusalProjectionInvalid, Message: "The desired Claude Desktop projection has no executable command, so nothing was written."}
	case errors.Is(err, ErrVerificationFailed):
		return &Refusal{Code: RefusalConfigUnmanageable, Message: "Claude Desktop configuration did not verify after writing, so no success is reported."}
	default:
		return &Refusal{Code: RefusalConfigUnmanageable, Message: "Claude Desktop configuration could not be written."}
	}
}
