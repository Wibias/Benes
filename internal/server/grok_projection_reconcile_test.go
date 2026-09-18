package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/catalog"
	"github.com/Wibias/Benes/internal/grok"
	"github.com/Wibias/Benes/internal/harnessboard"
	"github.com/Wibias/Benes/internal/modeldiscovery"
)

/*
Automatic Grok reconciliation.

These tests drive the real triggers rather than the reconcile helper: the catalogue is replaced
through replaceProviderCatalog (what the discovery sync calls) and the listener address arrives
through ReconcileAfterListen (what the serve lifecycle calls). Every assertion is on the Grok
config bytes, so nothing here can pass by rendering a claim in the dashboard.
*/

type grokFixture struct {
	handler *handler
	home    string
	config  string
}

func newGrokFixture(t *testing.T, models ...string) *grokFixture {
	t.Helper()
	grokHome := t.TempDir()
	t.Setenv("GROK_HOME", grokHome)
	benesHome := t.TempDir()
	configPath := filepath.Join(benesHome, "config.json")
	if err := os.WriteFile(configPath, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	rows := make([]catalog.Model, 0, len(models))
	for _, id := range models {
		rows = append(rows, catalog.Model{ID: id})
	}
	served, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
		CatalogModels:  rows,
		ConfigPath:     configPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	h, ok := served.(*handler)
	if !ok {
		t.Fatalf("unexpected handler type %T", served)
	}
	attachHandlerClose(t, h)
	return &grokFixture{handler: h, home: benesHome, config: filepath.Join(grokHome, "config.toml")}
}

// apply writes the projection the way the Harness apply action does, so a test starts from a real
// applied state rather than from a hand-written file.
func (f *grokFixture) apply(t *testing.T, host string, port int) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, "/api/native-integrations/grok", strings.NewReader("{\"enabled\":true}"))
	req.Host = host + ":" + strconv.Itoa(port)
	rr := httptest.NewRecorder()
	f.handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("apply status=%d body=%s", rr.Code, rr.Body.String())
	}
	return rr.Body.String()
}

func (f *grokFixture) bytes(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(f.config)
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		t.Fatal(err)
	}
	return string(raw)
}

func (f *grokFixture) registration(t *testing.T) map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/grok", nil)
	req.Host = "127.0.0.1:23100"
	rr := httptest.NewRecorder()
	f.handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	out := map[string]any{}
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func (f *grokFixture) setAutoApply(t *testing.T, autoApply bool) {
	t.Helper()
	row := harnessboard.DefaultSettings()
	row.AutoApply = autoApply
	if _, err := harnessboard.PutSettings(f.home, "grok", row); err != nil {
		t.Fatal(err)
	}
}

// replaceOpenAICatalog is the catalogue mutation the live discovery sync performs.
func (f *grokFixture) replaceOpenAICatalog(t *testing.T, models ...string) {
	t.Helper()
	entries := make([]modeldiscovery.CatalogEntry, 0, len(models))
	for _, id := range models {
		entries = append(entries, modeldiscovery.CatalogEntry{ID: strings.TrimPrefix(id, "openai/")})
	}
	f.handler.replaceProviderCatalog("openai", entries)
}

func TestCatalogueGrowthReconcilesAnAppliedGrokProjection(t *testing.T) {
	f := newGrokFixture(t, "openai/gpt-5.6-sol")
	f.handler.ReconcileAfterListen("127.0.0.1", 23100)
	if f.bytes(t) != "" {
		t.Fatalf("reconciliation created a projection nobody applied: %s", f.bytes(t))
	}
	f.apply(t, "127.0.0.1", 23100)
	if !strings.Contains(f.bytes(t), "model = \"openai/gpt-5.6-sol\"") {
		t.Fatalf("applied block=%s", f.bytes(t))
	}

	f.replaceOpenAICatalog(t, "openai/gpt-5.6-sol", "openai/gpt-5.7-sol")

	body := f.bytes(t)
	if !strings.Contains(body, "model = \"openai/gpt-5.7-sol\"") {
		t.Fatalf("catalogue growth did not reach the Grok config: %s", body)
	}
	registration := f.registration(t)
	if registration["registered"] != float64(2) || registration["current"] != true {
		t.Fatalf("registration=%v body=%s", registration, body)
	}
}

func TestCatalogueGrowthLeavesTheConfigAloneWhenAutoApplyIsOff(t *testing.T) {
	f := newGrokFixture(t, "openai/gpt-5.6-sol")
	f.apply(t, "127.0.0.1", 23100)
	// A bound address is what makes reconciliation possible at all, so the setting is the only
	// thing that can be stopping the write below.
	f.handler.ReconcileAfterListen("127.0.0.1", 23100)
	before := f.bytes(t)
	f.setAutoApply(t, false)

	f.replaceOpenAICatalog(t, "openai/gpt-5.6-sol", "openai/gpt-5.7-sol")

	if after := f.bytes(t); after != before {
		t.Fatalf("autoApply=false rewrote the config:\n%s\n---\n%s", before, after)
	}
	registration := f.registration(t)
	if registration["current"] != false {
		t.Fatalf("a manual harness must report out of date: %v", registration)
	}
	if registration["registered"] != float64(1) || registration["catalogue"] != float64(2) {
		t.Fatalf("registration=%v", registration)
	}

	// Positive control: the same mutation does reconcile once autoApply is back on.
	f.setAutoApply(t, true)
	f.replaceOpenAICatalog(t, "openai/gpt-5.6-sol", "openai/gpt-5.7-sol")
	if !strings.Contains(f.bytes(t), "model = \"openai/gpt-5.7-sol\"") {
		t.Fatalf("autoApply=true did not reconcile: %s", f.bytes(t))
	}
	f.setAutoApply(t, false)
	if err := os.WriteFile(f.config, []byte(before), 0o600); err != nil {
		t.Fatal(err)
	}

	// The normal re-apply is still what writes the current catalogue.
	f.apply(t, "127.0.0.1", 23100)
	if !strings.Contains(f.bytes(t), "model = \"openai/gpt-5.7-sol\"") {
		t.Fatalf("manual re-apply did not write the current catalogue: %s", f.bytes(t))
	}
	if f.registration(t)["current"] != true {
		t.Fatalf("registration=%v", f.registration(t))
	}
}

func TestListenAddressChangeRebuildsAnAppliedProjection(t *testing.T) {
	f := newGrokFixture(t, "openai/gpt-5.6-sol")
	f.apply(t, "127.0.0.1", 1111)
	if !strings.Contains(f.bytes(t), "http://127.0.0.1:1111/v1") {
		t.Fatalf("block=%s", f.bytes(t))
	}

	f.handler.ReconcileAfterListen("127.0.0.1", 2222)

	if !strings.Contains(f.bytes(t), "http://127.0.0.1:2222/v1") {
		t.Fatalf("listener address change did not reach the block: %s", f.bytes(t))
	}
}

func TestWildcardBindReconcilesToLoopback(t *testing.T) {
	f := newGrokFixture(t, "openai/gpt-5.6-sol")
	f.apply(t, "127.0.0.1", 23100)

	f.handler.ReconcileAfterListen("0.0.0.0", 23100)

	// A wildcard bind serves loopback too, so the projection stays usable instead of stripped.
	if !strings.Contains(f.bytes(t), "http://127.0.0.1:23100/v1") {
		t.Fatalf("block=%s", f.bytes(t))
	}
}

func TestNonLoopbackBindKeepsTheCanonicalSafetyRule(t *testing.T) {
	f := newGrokFixture(t, "openai/gpt-5.6-sol")
	f.apply(t, "127.0.0.1", 23100)

	f.handler.ReconcileAfterListen("192.168.1.5", 23100)

	// The writer's own loopback rule decides this, and the outcome is recorded rather than
	// reported as a successful reconcile.
	if strings.Contains(f.bytes(t), grok.BeginMarker) {
		t.Fatalf("non-loopback bind left a loopback block behind: %s", f.bytes(t))
	}
	if msg, _ := f.registration(t)["lastAutomaticError"].(string); !strings.Contains(msg, "non-loopback") {
		t.Fatalf("failure not reported: %v", f.registration(t))
	}
}

func TestAutomaticReconcileUsesTheSameWriterAsManualApply(t *testing.T) {
	manual := newGrokFixture(t, "openai/gpt-5.6-sol")
	manual.apply(t, "127.0.0.1", 23100)
	manual.handler.catalogModels = append(manual.handler.catalogModels, catalog.Model{ID: "openai/gpt-5.7-sol"})
	manual.apply(t, "127.0.0.1", 23100)

	automatic := newGrokFixture(t, "openai/gpt-5.6-sol")
	automatic.apply(t, "127.0.0.1", 23100)
	automatic.handler.ReconcileAfterListen("127.0.0.1", 23100)
	automatic.replaceOpenAICatalog(t, "openai/gpt-5.6-sol", "openai/gpt-5.7-sol")

	if manual.bytes(t) != automatic.bytes(t) {
		t.Fatalf("two writers produced different configs:\n%s\n---\n%s", manual.bytes(t), automatic.bytes(t))
	}
}

func TestAutomaticReconcileIsFailClosedOnAnOrphanedMarker(t *testing.T) {
	f := newGrokFixture(t, "openai/gpt-5.6-sol")
	f.apply(t, "127.0.0.1", 23100)
	f.handler.ReconcileAfterListen("127.0.0.1", 23100)
	orphaned := strings.Replace(f.bytes(t), grok.EndMarker, "", 1)
	if err := os.WriteFile(f.config, []byte(orphaned), 0o600); err != nil {
		t.Fatal(err)
	}

	// A catalogue change must not panic the listener, and must not rewrite a config it cannot
	// reason about.
	f.replaceOpenAICatalog(t, "openai/gpt-5.6-sol", "openai/gpt-5.7-sol")

	if after := f.bytes(t); after != orphaned {
		t.Fatalf("orphaned config was rewritten:\n%s\n---\n%s", orphaned, after)
	}
	registration := f.registration(t)
	if registration["current"] != false {
		t.Fatalf("an orphaned marker must not read as current: %v", registration)
	}
	// The region is unreadable, so the harness reads as not applied and reconciliation leaves it
	// strictly alone; the refusal itself still reaches the user through the normal Apply action.
	if registration["present"] != false {
		t.Fatalf("unreadable region must not read as present: %v", registration)
	}
	if body := f.apply(t, "127.0.0.1", 23100); !strings.Contains(body, "orphaned") {
		t.Fatalf("explicit apply did not surface the refusal: %s", body)
	}
	if after := f.bytes(t); after != orphaned {
		t.Fatalf("refused apply rewrote the config:\n%s\n---\n%s", orphaned, after)
	}
}

func TestReconcileNeverEnablesAnUnappliedProjection(t *testing.T) {
	f := newGrokFixture(t, "openai/gpt-5.6-sol")
	f.handler.ReconcileAfterListen("127.0.0.1", 23100)

	f.replaceOpenAICatalog(t, "openai/gpt-5.6-sol", "openai/gpt-5.7-sol")

	if body := f.bytes(t); strings.Contains(body, grok.BeginMarker) {
		t.Fatalf("a catalogue change enabled an unapplied projection: %s", body)
	}
}

func TestReconcileDoesNothingBeforeAnAddressIsKnown(t *testing.T) {
	f := newGrokFixture(t, "openai/gpt-5.6-sol")
	f.apply(t, "127.0.0.1", 23100)
	before := f.bytes(t)

	// No listener address yet: a catalogue change must not write a block that points nowhere.
	f.replaceOpenAICatalog(t, "openai/gpt-5.6-sol", "openai/gpt-5.7-sol")

	if after := f.bytes(t); after != before {
		t.Fatalf("wrote an addressless projection:\n%s\n---\n%s", before, after)
	}
}

func TestUnreadableHarnessSettingsFailClosed(t *testing.T) {
	f := newGrokFixture(t, "openai/gpt-5.6-sol")
	f.apply(t, "127.0.0.1", 23100)
	f.handler.ReconcileAfterListen("127.0.0.1", 23100)
	before := f.bytes(t)
	if err := os.WriteFile(harnessboard.SettingsPath(f.home), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	f.replaceOpenAICatalog(t, "openai/gpt-5.6-sol", "openai/gpt-5.7-sol")

	if after := f.bytes(t); after != before {
		t.Fatalf("unreadable settings rewrote the config:\n%s\n---\n%s", before, after)
	}
}
