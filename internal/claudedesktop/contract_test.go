package claudedesktop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fixture struct {
	home string
	env  map[string]string
	path string
}

// installedHost is a temporary Windows-shaped home whose Claude Desktop
// application data directory exists. No test ever reaches the real user home.
func installedHost(t *testing.T) fixture {
	t.Helper()
	home := t.TempDir()
	path := configPathForHome(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	return fixture{home: home, env: windowsHomeEnv(home), path: path}
}

func (f fixture) input(managed *Projection, enabled bool) Input {
	return Input{
		Host:           "windows",
		Home:           f.home,
		Env:            f.env,
		LookPath:       missLookPath,
		DesiredEnabled: enabled,
		Managed:        managed,
	}
}

func statusJSON(t *testing.T, status Status) string {
	t.Helper()
	body, err := json.Marshal(status)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func TestStatusUnsupportedHostFailsClosed(t *testing.T) {
	status := Evaluate(Input{Host: "linux", Home: t.TempDir(), LookPath: missLookPath})
	if status.HostSupported || status.Installed || status.Configurable {
		t.Fatalf("status=%+v", status)
	}
	if status.State != StateUnsupportedHost {
		t.Fatalf("state=%q", status.State)
	}
	if status.Refusal == nil || status.Refusal.Code != RefusalUnsupportedHost {
		t.Fatalf("refusal=%+v", status.Refusal)
	}
	if status.Applied || status.Stale || status.DesiredFingerprint != "" || status.AppliedFingerprint != "" {
		t.Fatalf("unsupported host reported native state: %+v", status)
	}
}

func TestStatusNotInstalled(t *testing.T) {
	home := t.TempDir()
	status := Evaluate(Input{
		Host:     "windows",
		Home:     home,
		Env:      windowsHomeEnv(home),
		LookPath: missLookPath,
	})
	if status.State != StateNotInstalled || status.Installed || status.Configurable {
		t.Fatalf("status=%+v", status)
	}
	if status.Refusal == nil || status.Refusal.Code != RefusalNotInstalled {
		t.Fatalf("refusal=%+v", status.Refusal)
	}
	if status.ObservedKind != ObservedUnobserved {
		t.Fatalf("observed=%q", status.ObservedKind)
	}
}

func TestStatusMalformedConfigurationIsUnmanageable(t *testing.T) {
	host := installedHost(t)
	writeConfigFile(t, host.path, `{"mcpServers":`)
	status := Evaluate(host.input(&managedFixture, true))
	if status.State != StateConfigUnavailable || status.Configurable {
		t.Fatalf("status=%+v", status)
	}
	if status.ObservedKind != ObservedConfigUnparsable {
		t.Fatalf("observed=%q", status.ObservedKind)
	}
	if status.Refusal == nil || status.Refusal.Code != RefusalConfigMalformed {
		t.Fatalf("refusal=%+v", status.Refusal)
	}
}

// TestStatusWithoutManagedProjectionNeverClaimsApplied is the core contract
// assertion: a saved Benes preference, an enabled toggle, and even a native
// benes entry cannot make Claude Desktop report applied while Benes ships no
// applicable managed projection.
func TestStatusWithoutManagedProjectionNeverClaimsApplied(t *testing.T) {
	cases := []struct {
		name    string
		enabled bool
		body    string
	}{
		{"desired disabled", false, `{"coworkUserFilesPath":"/x"}`},
		{"desired enabled", true, ""},
		{"leftover benes entry", true, `{"mcpServers":{"benes":{"command":"benes","args":["mcp"]}}}`},
		{"unusable benes entry", true, `{"mcpServers":{"benes":"junk"}}`},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			host := installedHost(t)
			if testCase.body != "" {
				writeConfigFile(t, host.path, testCase.body)
			}
			status := Evaluate(host.input(ManagedNativeProjection(), testCase.enabled))
			if status.State != StateNoManagedProjection {
				t.Fatalf("state=%q", status.State)
			}
			if status.ManagedProjectionAvailable {
				t.Fatalf("managed projection reported available: %+v", status)
			}
			if status.Applied || status.Stale {
				t.Fatalf("status claimed native state: %+v", status)
			}
			if status.DesiredFingerprint != "" || status.AppliedFingerprint != "" {
				t.Fatalf("status invented fingerprints: %+v", status)
			}
			if !status.Configurable {
				t.Fatalf("status=%+v", status)
			}
			if status.ConfigPath != host.path {
				t.Fatalf("configPath=%q want=%q", status.ConfigPath, host.path)
			}
			if status.Refusal == nil || status.Refusal.Code != RefusalRuntimeUnavailable {
				t.Fatalf("refusal=%+v", status.Refusal)
			}
			if status.DesiredEnabled != testCase.enabled {
				t.Fatalf("desiredEnabled=%v want=%v", status.DesiredEnabled, testCase.enabled)
			}
		})
	}
}

func TestStatusDesiredButNotApplied(t *testing.T) {
	host := installedHost(t)
	status := Evaluate(host.input(&managedFixture, true))
	if status.State != StateNotApplied || status.Applied || status.Stale {
		t.Fatalf("status=%+v", status)
	}
	if status.ObservedKind != ObservedConfigAbsent {
		t.Fatalf("observed=%q", status.ObservedKind)
	}
	if status.DesiredFingerprint != managedFixture.Fingerprint() {
		t.Fatalf("desiredFingerprint=%q", status.DesiredFingerprint)
	}
	if status.AppliedFingerprint != "" || status.ObservedFingerprint != "" {
		t.Fatalf("status invented native fingerprints: %+v", status)
	}
	if status.Refusal != nil {
		t.Fatalf("refusal=%+v", status.Refusal)
	}
}

func TestStatusAppliedWhenNativeProjectionMatchesDesired(t *testing.T) {
	host := installedHost(t)
	writeConfigFile(t, host.path, `{"mcpServers":{"benes":{"command":"benes","args":["mcp","serve"]}}}`)
	status := Evaluate(host.input(&managedFixture, true))
	if status.State != StateApplied || !status.Applied || status.Stale {
		t.Fatalf("status=%+v", status)
	}
	if status.AppliedFingerprint != managedFixture.Fingerprint() || status.ObservedFingerprint != status.AppliedFingerprint {
		t.Fatalf("fingerprints=%+v", status)
	}
	if status.RestartRequired {
		t.Fatal("status invented a restart requirement")
	}
}

func TestStatusStaleWhenManagedProjectionDiffers(t *testing.T) {
	host := installedHost(t)
	writeConfigFile(t, host.path, `{"mcpServers":{"benes":{"command":"benes","args":["mcp"]}}}`)
	status := Evaluate(host.input(&managedFixture, true))
	if status.State != StateStale || status.Applied || !status.Stale {
		t.Fatalf("status=%+v", status)
	}
	if status.AppliedFingerprint != "" {
		t.Fatalf("stale status reported an applied fingerprint: %+v", status)
	}
	if status.ObservedFingerprint == "" || status.ObservedFingerprint == status.DesiredFingerprint {
		t.Fatalf("observedFingerprint=%q", status.ObservedFingerprint)
	}
}

func TestStatusStaleWhenOnlyTheManagedEntryWasEditedExternally(t *testing.T) {
	host := installedHost(t)
	writeConfigFile(t, host.path, `{"mcpServers":{"benes":{"command":"benes","args":["mcp","serve"]}}}`)
	if status := Evaluate(host.input(&managedFixture, true)); status.State != StateApplied {
		t.Fatalf("precondition state=%q", status.State)
	}
	writeConfigFile(t, host.path, `{"mcpServers":{"benes":{"command":"benes","args":["mcp","serve","--evil"]}}}`)
	status := Evaluate(host.input(&managedFixture, true))
	if status.State != StateStale || !status.Stale || status.Applied {
		t.Fatalf("external edit was not detected: %+v", status)
	}
}

func TestUnrelatedUserEditsDoNotMakeTheProjectionStale(t *testing.T) {
	host := installedHost(t)
	writeConfigFile(t, host.path, `{"mcpServers":{"benes":{"command":"benes","args":["mcp","serve"]}}}`)
	before := Evaluate(host.input(&managedFixture, true))
	if before.State != StateApplied {
		t.Fatalf("precondition state=%q", before.State)
	}
	writeConfigFile(t, host.path, `{
	  "coworkUserFilesPath": "/home/user/Claude",
	  "preferences": {"theme": "light"},
	  "mcpServers": {
	    "other": {"command": "other-server"},
	    "benes": {"command": "benes", "args": ["mcp", "serve"]}
	  }
	}`)
	after := Evaluate(host.input(&managedFixture, true))
	if after.State != StateApplied || after.Stale || !after.Applied {
		t.Fatalf("unrelated edits caused staleness: %+v", after)
	}
	if after.AppliedFingerprint != before.AppliedFingerprint {
		t.Fatalf("fingerprint changed: %q -> %q", before.AppliedFingerprint, after.AppliedFingerprint)
	}
}

func TestFormattingOnlyRewriteKeepsAppliedState(t *testing.T) {
	host := installedHost(t)
	writeConfigFile(t, host.path, `{"mcpServers":{"benes":{"command":"benes","args":["mcp","serve"]}}}`)
	before := Evaluate(host.input(&managedFixture, true))
	writeConfigFile(t, host.path, "{\n  \"mcpServers\" : {\n    \"benes\" : { \"args\" : [ \"mcp\" , \"serve\" ] , \"command\" : \"benes\" }\n  }\n}\n")
	after := Evaluate(host.input(&managedFixture, true))
	if after.State != StateApplied || after.Stale {
		t.Fatalf("formatting rewrite changed semantic state: %+v", after)
	}
	if after.AppliedFingerprint != before.AppliedFingerprint {
		t.Fatalf("fingerprint drifted: %q -> %q", before.AppliedFingerprint, after.AppliedFingerprint)
	}
}

func TestApplyRefusesAndLeavesTheNativeFileUntouched(t *testing.T) {
	cases := []struct {
		name    string
		build   func(t *testing.T) (Input, string)
		code    RefusalCode
		managed *Projection
	}{
		{
			name: "unsupported host",
			build: func(t *testing.T) (Input, string) {
				home := t.TempDir()
				return Input{Host: "linux", Home: home, LookPath: missLookPath}, filepath.Join(home, "claude_desktop_config.json")
			},
			code: RefusalUnsupportedHost,
		},
		{
			name: "not installed",
			build: func(t *testing.T) (Input, string) {
				home := t.TempDir()
				return Input{Host: "windows", Home: home, Env: windowsHomeEnv(home), LookPath: missLookPath}, filepath.Join(home, "claude_desktop_config.json")
			},
			code: RefusalNotInstalled,
		},
		{
			name: "no managed projection",
			build: func(t *testing.T) (Input, string) {
				host := installedHost(t)
				writeConfigFile(t, host.path, userConfigFixture)
				return host.input(ManagedNativeProjection(), true), host.path
			},
			code: RefusalRuntimeUnavailable,
		},
		{
			name: "desired disabled",
			build: func(t *testing.T) (Input, string) {
				host := installedHost(t)
				writeConfigFile(t, host.path, userConfigFixture)
				return host.input(&managedFixture, false), host.path
			},
			code:    RefusalDesiredDisabled,
			managed: &managedFixture,
		},
		{
			name: "malformed configuration",
			build: func(t *testing.T) (Input, string) {
				host := installedHost(t)
				writeConfigFile(t, host.path, `{"mcpServers":`)
				return host.input(&managedFixture, true), host.path
			},
			code:    RefusalConfigMalformed,
			managed: &managedFixture,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			in, path := testCase.build(t)
			before := ""
			if raw, err := os.ReadFile(path); err == nil {
				before = string(raw)
			}
			result, refusal := Apply(in)
			if refusal == nil || refusal.Code != testCase.code {
				t.Fatalf("refusal=%+v", refusal)
			}
			if result.Changed || result.Applied || result.RestartRequired {
				t.Fatalf("refused apply reported success: %+v", result)
			}
			after := ""
			if raw, err := os.ReadFile(path); err == nil {
				after = string(raw)
			}
			if after != before {
				t.Fatalf("refused apply mutated the file:\n%s", after)
			}
		})
	}
}

func TestApplyWritesTheVerifiedProjectionWhenOneExists(t *testing.T) {
	host := installedHost(t)
	writeConfigFile(t, host.path, userConfigFixture)
	result, refusal := Apply(host.input(&managedFixture, true))
	if refusal != nil {
		t.Fatalf("refusal=%+v", refusal)
	}
	if !result.Changed || !result.Applied || !result.RestartRequired {
		t.Fatalf("result=%+v", result)
	}
	status := Evaluate(host.input(&managedFixture, true))
	if status.State != StateApplied || !status.Applied || status.Stale {
		t.Fatalf("status=%+v", status)
	}
	assertUnrelatedPreserved(t, host.path)
}

func TestApplyIsIdempotent(t *testing.T) {
	host := installedHost(t)
	if _, refusal := Apply(host.input(&managedFixture, true)); refusal != nil {
		t.Fatalf("refusal=%+v", refusal)
	}
	before := readConfigFile(t, host.path)
	result, refusal := Apply(host.input(&managedFixture, true))
	if refusal != nil {
		t.Fatalf("refusal=%+v", refusal)
	}
	if result.Changed || !result.Applied {
		t.Fatalf("result=%+v", result)
	}
	if after := readConfigFile(t, host.path); after != before {
		t.Fatal("re-apply churned the native file")
	}
}

func TestApplyRefusesAnInvalidDesiredProjection(t *testing.T) {
	host := installedHost(t)
	writeConfigFile(t, host.path, userConfigFixture)
	before := readConfigFile(t, host.path)
	invalid := Projection{Command: "   "}
	result, refusal := Apply(host.input(&invalid, true))
	if refusal == nil || refusal.Code != RefusalProjectionInvalid {
		t.Fatalf("refusal=%+v", refusal)
	}
	if result.Changed || result.Applied {
		t.Fatalf("invalid projection reported success: %+v", result)
	}
	if after := readConfigFile(t, host.path); after != before {
		t.Fatalf("invalid projection mutated native configuration: %s", after)
	}
}

func TestDisableWithoutManagedProjectionIsATruthfulNoOp(t *testing.T) {
	host := installedHost(t)
	writeConfigFile(t, host.path, userConfigFixture)
	before := readConfigFile(t, host.path)
	result, refusal := Disable(host.input(ManagedNativeProjection(), true))
	if refusal != nil {
		t.Fatalf("refusal=%+v", refusal)
	}
	if result.Changed {
		t.Fatalf("result=%+v", result)
	}
	if after := readConfigFile(t, host.path); after != before {
		t.Fatal("no-op disable rewrote the file")
	}
}

func TestDisableRemovesOnlyTheExactProjection(t *testing.T) {
	host := installedHost(t)
	writeConfigFile(t, host.path, userConfigFixture)
	if _, refusal := Apply(host.input(&managedFixture, true)); refusal != nil {
		t.Fatalf("refusal=%+v", refusal)
	}
	result, refusal := Disable(host.input(&managedFixture, false))
	if refusal != nil {
		t.Fatalf("refusal=%+v", refusal)
	}
	if !result.Changed || result.Applied || !result.RestartRequired {
		t.Fatalf("result=%+v", result)
	}
	assertUnrelatedPreserved(t, host.path)
	if kind := mustObservePath(t, host.path).Kind; kind != ObservedNoBenesEntry {
		t.Fatalf("observed=%q", kind)
	}
	again, refusal := Disable(host.input(&managedFixture, false))
	if refusal != nil {
		t.Fatalf("refusal=%+v", refusal)
	}
	if again.Changed {
		t.Fatalf("disable was not idempotent: %+v", again)
	}
}

func TestDisableRefusesAnEntryBenesCannotProveItWrote(t *testing.T) {
	host := installedHost(t)
	writeConfigFile(t, host.path, `{"mcpServers":{"benes":{"command":"user-owned","args":[]}}}`)
	before := readConfigFile(t, host.path)
	result, refusal := Disable(host.input(&managedFixture, false))
	if refusal == nil {
		t.Fatalf("result=%+v", result)
	}
	if result.Changed {
		t.Fatalf("refused disable reported a change: %+v", result)
	}
	if after := readConfigFile(t, host.path); after != before {
		t.Fatalf("refused disable rewrote the file:\n%s", after)
	}
}

func TestDisableRefusesUnsupportedHostAndMissingInstall(t *testing.T) {
	if _, refusal := Disable(Input{Host: "linux", Home: t.TempDir(), LookPath: missLookPath, Managed: &managedFixture}); refusal == nil || refusal.Code != RefusalUnsupportedHost {
		t.Fatalf("unsupported host refusal=%+v", refusal)
	}
	home := t.TempDir()
	notInstalled := Input{Host: "windows", Home: home, Env: windowsHomeEnv(home), LookPath: missLookPath, Managed: &managedFixture}
	if _, refusal := Disable(notInstalled); refusal == nil || refusal.Code != RefusalNotInstalled {
		t.Fatalf("not installed refusal=%+v", refusal)
	}
}

func TestStatusCarriesNoSecretAndNoLegacyContractFields(t *testing.T) {
	const secret = "sk-ant-api03-desktop-secret"
	host := installedHost(t)
	writeConfigFile(t, host.path, `{
	  "credentials": {"apiKey": "`+secret+`"},
	  "mcpServers": {
	    "other": {"command": "other-server", "env": {"TOKEN": "`+secret+`"}}
	  }
	}`)
	for _, status := range []Status{
		Evaluate(host.input(ManagedNativeProjection(), true)),
		Evaluate(host.input(&managedFixture, true)),
	} {
		body := statusJSON(t, status)
		if json.Valid([]byte(body)) == false {
			t.Fatalf("status is not JSON: %s", body)
		}
		if strings.Contains(body, secret) {
			t.Fatalf("status leaked a secret: %s", body)
		}
		for _, token := range []string{
			`"model"`, `"modelMap"`, `"tierModels"`, `"assignments"`, `"defaults"`,
			`"activeProfile"`, `"appliedAt"`, `"apiKey"`, `"api_key"`, `"token"`,
			`"baseUrl"`, `"base_url"`, `"endpoint"`,
		} {
			if strings.Contains(body, token) {
				t.Fatalf("status carries unsupported field %s: %s", token, body)
			}
		}
		var decoded map[string]any
		if err := json.Unmarshal([]byte(body), &decoded); err != nil {
			t.Fatal(err)
		}
		for key := range decoded {
			if !canonicalStatusKeys[key] {
				t.Fatalf("status carries non-canonical field %q: %s", key, body)
			}
		}
	}
}

var canonicalStatusKeys = map[string]bool{
	"clientId": true, "hostSupported": true, "installed": true, "configurable": true,
	"configPath": true, "state": true, "managedProjectionAvailable": true,
	"desiredEnabled": true, "desiredFingerprint": true, "observedKind": true,
	"applied": true, "appliedFingerprint": true, "observedFingerprint": true,
	"stale": true, "restartRequired": true, "refusal": true,
}

func TestEvaluateIsReadOnly(t *testing.T) {
	host := installedHost(t)
	writeConfigFile(t, host.path, userConfigFixture)
	before := readConfigFile(t, host.path)
	Evaluate(host.input(ManagedNativeProjection(), true))
	Evaluate(host.input(&managedFixture, true))
	if after := readConfigFile(t, host.path); after != before {
		t.Fatal("evaluate mutated the native file")
	}
}

func TestEvaluateRefusesUnreadableConfiguration(t *testing.T) {
	home := t.TempDir()
	path := configPathForHome(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
	status := Evaluate(Input{Host: "windows", Home: home, Env: windowsHomeEnv(home), LookPath: missLookPath})
	if status.State != StateConfigUnavailable || status.Configurable {
		t.Fatalf("status=%+v", status)
	}
	if status.Refusal == nil || status.Refusal.Code != RefusalConfigUnmanageable {
		t.Fatalf("refusal=%+v", status.Refusal)
	}
	unmanageable := Input{Host: "windows", Home: home, Env: windowsHomeEnv(home), LookPath: missLookPath, DesiredEnabled: true, Managed: &managedFixture}
	result, refusal := Apply(unmanageable)
	if refusal == nil || refusal.Code != RefusalConfigUnmanageable {
		t.Fatalf("refusal=%+v", refusal)
	}
	if result.Changed || result.Applied {
		t.Fatalf("unmanageable apply reported success: %+v", result)
	}
	disabled, refusal := Disable(unmanageable)
	if refusal == nil || refusal.Code != RefusalConfigUnmanageable {
		t.Fatalf("disable refusal=%+v", refusal)
	}
	if disabled.Changed {
		t.Fatalf("unmanageable disable reported a change: %+v", disabled)
	}
}
