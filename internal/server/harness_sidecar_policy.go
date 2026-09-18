package server

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/Wibias/Benes/internal/harnessboard"
	"github.com/Wibias/Benes/internal/harnesspolicy"
)

type harnessSidecarCapabilities struct {
	IdentityStampable bool `json:"identityStampable"`
	WebSearch         bool `json:"webSearch"`
	Vision            bool `json:"vision"`
}

var harnessSidecarRegistry = map[string]harnessSidecarCapabilities{
	"claude-desktop": {},
	"claude":         {},
	"codex":          {IdentityStampable: true, WebSearch: true, Vision: true},
	"dsh":            {IdentityStampable: true, WebSearch: true, Vision: true},
	"opencode":       {IdentityStampable: true, WebSearch: true, Vision: true},
	"pi":             {},
	"prime":          {},
	"omp":            {},
	"hermes":         {IdentityStampable: true, WebSearch: true, Vision: true},
	"openclaw":       {IdentityStampable: true, WebSearch: true, Vision: true},
	"kimi":           {},
	"gajae":          {},
	"grok":           {IdentityStampable: true, WebSearch: true, Vision: true},
	"mcode":          {},
}

func validateHarnessSidecarCapabilities(id string, caps harnessSidecarCapabilities) error {
	if !harnessboard.Known(id) {
		return fmt.Errorf("unknown harness %q", id)
	}
	if !caps.IdentityStampable && (caps.WebSearch || caps.Vision) {
		return fmt.Errorf("harness %q cannot expose sidecar capabilities without identity stamping", id)
	}
	return nil
}

func harnessSidecarCapabilitiesFor(id string) (harnessSidecarCapabilities, bool) {
	caps, ok := harnessSidecarRegistry[id]
	if !ok || validateHarnessSidecarCapabilities(id, caps) != nil {
		return harnessSidecarCapabilities{}, false
	}
	return caps, true
}

type harnessSidecarSnapshot struct {
	overrides map[string]harnesspolicy.Overrides
	valid     bool
}

type harnessSidecarRuntime struct {
	snapshot atomic.Pointer[harnessSidecarSnapshot]
}

func newHarnessSidecarRuntime(settings map[string]harnessboard.Settings, loadErr error) *harnessSidecarRuntime {
	runtime := &harnessSidecarRuntime{}
	if loadErr != nil {
		runtime.snapshot.Store(&harnessSidecarSnapshot{overrides: map[string]harnesspolicy.Overrides{}, valid: false})
		return runtime
	}
	runtime.snapshot.Store(newHarnessSidecarSnapshot(settings))
	return runtime
}

func newHarnessSidecarSnapshot(settings map[string]harnessboard.Settings) *harnessSidecarSnapshot {
	overrides := make(map[string]harnesspolicy.Overrides)
	for id, row := range settings {
		if !harnessboard.Known(id) || row.Sidecars == nil {
			continue
		}
		cloned := cloneHarnessSidecarOverrides(*row.Sidecars)
		if cloned.WebSearch == nil && cloned.Vision == nil {
			continue
		}
		overrides[id] = cloned
	}
	return &harnessSidecarSnapshot{overrides: overrides, valid: true}
}

func (runtime *harnessSidecarRuntime) Valid() bool {
	if runtime == nil {
		return false
	}
	snapshot := runtime.snapshot.Load()
	return snapshot != nil && snapshot.valid
}

func (runtime *harnessSidecarRuntime) Overrides(id string) (harnesspolicy.Overrides, bool) {
	if runtime == nil {
		return harnesspolicy.Overrides{}, false
	}
	snapshot := runtime.snapshot.Load()
	if snapshot == nil || !snapshot.valid {
		return harnesspolicy.Overrides{}, false
	}
	overrides, ok := snapshot.overrides[id]
	if !ok {
		return harnesspolicy.Overrides{}, false
	}
	return cloneHarnessSidecarOverrides(overrides), true
}

func (runtime *harnessSidecarRuntime) Replace(settings map[string]harnessboard.Settings) {
	if runtime == nil {
		return
	}
	runtime.snapshot.Store(newHarnessSidecarSnapshot(settings))
}

func cloneHarnessSidecarOverrides(overrides harnesspolicy.Overrides) harnesspolicy.Overrides {
	return harnesspolicy.Overrides{
		WebSearch: cloneHarnessSidecarActivation(overrides.WebSearch),
		Vision:    cloneHarnessSidecarActivation(overrides.Vision),
	}
}

func cloneHarnessSidecarActivation(value *harnesspolicy.Activation) *harnesspolicy.Activation {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

var harnessSidecarRuntimes sync.Map // map[*handler]*harnessSidecarRuntime

func harnessSidecarRuntimeFor(h *handler) *harnessSidecarRuntime {
	if h == nil {
		return newHarnessSidecarRuntime(nil, fmt.Errorf("handler is required"))
	}
	if current, ok := harnessSidecarRuntimes.Load(h); ok {
		return current.(*harnessSidecarRuntime)
	}
	home := strings.TrimSpace(h.benesHome())
	if home == "" {
		candidate := newHarnessSidecarRuntime(nil, fmt.Errorf("config path is required"))
		actual, _ := harnessSidecarRuntimes.LoadOrStore(h, candidate)
		return actual.(*harnessSidecarRuntime)
	}
	settings, err := harnessboard.LoadSettings(home)
	candidate := newHarnessSidecarRuntime(settings, err)
	actual, _ := harnessSidecarRuntimes.LoadOrStore(h, candidate)
	return actual.(*harnessSidecarRuntime)
}

func forgetHarnessSidecarRuntime(h *handler) {
	if h != nil {
		harnessSidecarRuntimes.Delete(h)
	}
}

type configuredHarnessSidecar struct {
	Enabled bool                 `json:"enabled"`
	Source  harnesspolicy.Source `json:"source"`
}

type configuredHarnessSidecarPolicy struct {
	WebSearch configuredHarnessSidecar `json:"webSearch"`
	Vision    configuredHarnessSidecar `json:"vision"`
}

type harnessSidecarPolicyView struct {
	Capabilities harnessSidecarCapabilities     `json:"capabilities"`
	Configured   configuredHarnessSidecarPolicy `json:"configured"`
}

func (h *handler) configuredHarnessSidecarPolicy(id string) configuredHarnessSidecarPolicy {
	root := h.loadConfigRoot()
	if root == nil {
		root = map[string]any{}
	}
	webSearchSection, _ := root["webSearchSidecar"].(map[string]any)
	visionSection, _ := root["visionSidecar"].(map[string]any)

	var overrides harnesspolicy.Overrides
	if current, ok := harnessSidecarRuntimeFor(h).Overrides(id); ok {
		overrides = current
	}

	return configuredHarnessSidecarPolicy{
		WebSearch: configuredHarnessSidecarState(sidecarSectionEnabled(webSearchSection), overrides.WebSearch),
		Vision:    configuredHarnessSidecarState(sidecarSectionEnabled(visionSection), overrides.Vision),
	}
}

func configuredHarnessSidecarState(globalEnabled bool, override *harnesspolicy.Activation) configuredHarnessSidecar {
	decision, err := harnesspolicy.Resolve(harnesspolicy.ResolveInput{
		GlobalEnabled: globalEnabled,
		Override:      override,
		SidecarProven: true,
	})
	if err != nil {
		return configuredHarnessSidecar{Source: harnesspolicy.SourceUnsupported}
	}
	return configuredHarnessSidecar{Enabled: decision.Enabled, Source: decision.Source}
}

func (h *handler) harnessSidecarPolicyView(id string) harnessSidecarPolicyView {
	caps, ok := harnessSidecarCapabilitiesFor(id)
	if !ok {
		caps = harnessSidecarCapabilities{}
	}
	return harnessSidecarPolicyView{
		Capabilities: caps,
		Configured:   h.configuredHarnessSidecarPolicy(id),
	}
}

type harnessClientView struct {
	harnessboard.Client
	SidecarPolicy harnessSidecarPolicyView `json:"sidecarPolicy"`
}
