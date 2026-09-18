# Lab v1 and Fabric v1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship opt-in Compatibility Lab (durable protocol-conformance runs via `benes lab rebuild`) and an opt-in Fabric Tasks board, without putting either on the default `benes start` path.

**Architecture:** Lab is a new `internal/lab` JSONL ledger plus CLI and a GET merge in `lab_api.go`. Fabric kernel and `/api/fabric/tasks*` already exist; this slice adds `GET`/`PUT /api/fabric-settings` and a dashboard page gated on `GET /api/fabric/status`. CPG, combo round-robin, live probes, and Codex/Claude/A2A stay untouched.

**Tech Stack:** Go data plane (`cmd/benes`, `internal/`), React dashboard (`gui/src`), English Starlight docs (`docs/`).

**Spec:** `docs/superpowers/specs/2026-09-02-lab-fabric-v1-design.md`

## Global Constraints

- Production runtime is Go. No TypeScript under `cmd/` or `internal/`.
- Do not import `internal/sidecar/fabric` from `internal/lab`.
- Do not call provider HTTP from Lab rebuild or GET. `internal/lab` must not import `net/http`.
- Combos stay failover only. Do not add round-robin.
- CPG stays on Control. Do not move it onto Routing, Lab, or Fabric.
- `benes start` / `serve` must not create `~/.benes/lab` or start Fabric workers.
- Management APIs stay loopback-gated and never return secrets, API keys, request bodies, or home paths.
- Dashboard copy goes through `gui/src/i18n/en.ts` then every locale module; render with `useT()`.
- Do not run `benes stop` (it restores Codex config). Prove runtime with an isolated listener if needed.
- Do not git commit unless the user explicitly asked to commit in that session. Skip every Commit step until then.
- Hosted GitHub Actions may be blocked. Local focused tests are the proof.

## File map

| Path | Role |
| --- | --- |
| `internal/lab/event.go` | Hash-chained event envelope (`lab.run.started`, `lab.verdict.recorded`) |
| `internal/lab/store.go` | Append-only `events.jsonl` under `filepath.Dir(configPath)/lab` |
| `internal/lab/subjects.go` | Concrete `provider/model` subjects from `config.DiskConfig` |
| `internal/lab/rebuild.go` | Classify subjects × `compat.Harnesses`; append run + verdicts |
| `internal/lab/projection.go` | Latest verdicts, observations, corruption count |
| `cmd/benes/lab.go` | `benes lab help\|rebuild\|status` |
| `internal/server/lab_api.go` | Merge CLAIMED/store protocol rows; observations; event GET |
| `internal/compat/matrix.go` | Add `VerdictClaimed = "CLAIMED"` |
| `internal/server/fabric_settings_api.go` | `GET`/`PUT /api/fabric-settings` `{enabled}` |
| `gui/src/pages/Tasks.tsx` | Tasks board |
| `gui/src/pages/control-board.tsx` | Fabric switch after CPG |
| `docs/src/content/docs/...` | Honest Lab + Fabric copy |

---

### Task 1: Lab ledger

**Files:**
- Create: `internal/lab/event.go`
- Create: `internal/lab/store.go`
- Test: `internal/lab/store_test.go`

**Interfaces:**
- Consumes: none
- Produces: `lab.Event`, `lab.KindRunStarted`, `lab.KindVerdictRecorded`, `lab.HashEvent`, `lab.NewStore(dir string) *Store`, `(*Store).Append(Event) error`, `(*Store).All() ([]Event, error)`, `ErrCorrupt`

- [ ] **Step 1: Write the failing test**

```go
package lab

import (
	"path/filepath"
	"testing"
)

func TestStoreAppendsHashChain(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	first := Event{EventKind: KindRunStarted, RecordedAt: 1, Producer: "benes-lab", ProducerVersion: "capability-v1"}
	if err := store.Append(first); err != nil {
		t.Fatal(err)
	}
	second := Event{
		EventKind: KindVerdictRecorded, RecordedAt: 2, Producer: "benes-lab", ProducerVersion: "capability-v1",
		SubjectID: "openai-apikey/gpt-5.4", EvidenceLayer: "protocol_conformance", SuiteID: "codex", Verdict: "VERIFIED",
	}
	if err := store.Append(second); err != nil {
		t.Fatal(err)
	}
	events, err := store.All()
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Sequence != 1 || events[1].PrevHash != events[0].EventHash || events[1].PrevHash == "" {
		t.Fatalf("events=%+v", events)
	}
}

func TestStoreAllReportsCorruptLedger(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")
	if err := os.WriteFile(path, []byte("not-json\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := NewStore(dir).All()
	if err == nil {
		t.Fatal("expected corrupt")
	}
}
```

Add `"os"` to the import list in the test file.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/lab -count=1`

Expected: FAIL because package `lab` does not exist.

- [ ] **Step 3: Write minimal implementation**

`internal/lab/event.go`:

```go
package lab

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

const (
	KindRunStarted      = "lab.run.started"
	KindVerdictRecorded = "lab.verdict.recorded"
	ProducerName        = "benes-lab"
)

type Event struct {
	EventID          string `json:"eventId"`
	EventKind        string `json:"eventKind"`
	Sequence         uint64 `json:"sequence"`
	RecordedAt       int64  `json:"recordedAt"`
	Producer         string `json:"producer"`
	ProducerVersion  string `json:"producerVersion"`
	SubjectID        string `json:"subjectId,omitempty"`
	EvidenceLayer    string `json:"evidenceLayer,omitempty"`
	SuiteID          string `json:"suiteId,omitempty"`
	Verdict          string `json:"verdict,omitempty"`
	Excluded         bool   `json:"excluded"`
	ExclusionReason  *string `json:"exclusionReason"`
	PrevHash         string `json:"prevHash"`
	EventHash        string `json:"eventHash"`
}

func HashEvent(ev Event) (string, error) {
	copy := ev
	copy.EventHash = ""
	raw, err := json.Marshal(copy)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func newEventID(seq uint64, recordedAt int64) string {
	return fmt.Sprintf("%d-%d", recordedAt, seq)
}
```

`internal/lab/store.go`:

```go
package lab

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var ErrCorrupt = errors.New("lab ledger corrupt")

type Store struct {
	dir string
	mu  sync.Mutex
}

func NewStore(dir string) *Store { return &Store{dir: dir} }

func (s *Store) path() string { return filepath.Join(s.dir, "events.jsonl") }

func (s *Store) Append(ev Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, err := s.readAllLocked()
	if err != nil {
		return err
	}
	ev.Sequence = uint64(len(existing) + 1)
	if len(existing) > 0 {
		ev.PrevHash = existing[len(existing)-1].EventHash
	}
	if ev.EventID == "" {
		ev.EventID = newEventID(ev.Sequence, ev.RecordedAt)
	}
	if ev.Producer == "" {
		ev.Producer = ProducerName
	}
	h, err := HashEvent(ev)
	if err != nil {
		return err
	}
	ev.EventHash = h
	line, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(s.path(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(line, '\n'))
	return err
}

func (s *Store) All() ([]Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readAllLocked()
}

func (s *Store) readAllLocked() ([]Event, error) {
	data, err := os.ReadFile(s.path())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []Event
	for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		if line == "" {
			continue
		}
		var ev Event
		if json.Unmarshal([]byte(line), &ev) != nil || ev.EventID == "" {
			return nil, ErrCorrupt
		}
		out = append(out, ev)
	}
	return out, nil
}
```

Do not log file contents. Do not write secrets into events.

- [ ] **Step 4: Run the tests and make sure they pass**

Run: `go test ./internal/lab -count=1`

Expected: PASS

- [ ] **Step 5: Commit**

Skip unless the user asked to commit.

---

### Task 2: Rebuild from disk config

**Files:**
- Modify: `internal/compat/matrix.go` (add `VerdictClaimed = "CLAIMED"`)
- Create: `internal/lab/subjects.go`
- Create: `internal/lab/rebuild.go`
- Create: `internal/lab/projection.go`
- Test: `internal/lab/rebuild_test.go`

**Interfaces:**
- Consumes: `lab.Store`, `compat.Classify`, `compat.Harnesses`, `compat.ProviderAdapter`, `modeldiscovery.CatalogsFromConfig`, `modeldiscovery.CatalogID`, `config.DiskConfig`
- Produces: `lab.Subject{ID, Provider, Model, Adapter string}`, `lab.SubjectsFromConfig(config.DiskConfig) []Subject`, `lab.Rebuild(dir string, disk config.DiskConfig, now int64) (Result, error)`, `lab.Result{EventCount, SubjectCount, VerdictCount int}`, `lab.LoadProjection(dir string) (Projection, error)`, `lab.Projection{Events []Event, Verdicts map[string]Event, Observations []Event, CorruptionCount int}`, `lab.VerdictKey(subjectID, suiteID string) string`

- [ ] **Step 1: Write the failing test**

```go
package lab

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/config"
)

func TestRebuildWritesProtocolVerdictsWithoutHTTP(t *testing.T) {
	dir := t.TempDir()
	raw := []byte(`{
		"providers": {
			"openai-apikey": {"adapter":"openai-responses","models":["gpt-5.4","gpt-4o"]},
			"anthropic": {"adapter":"anthropic","models":["claude-sonnet-4"]}
		}
	}`)
	disk, err := decodeDisk(raw)
	if err != nil {
		t.Fatal(err)
	}
	result, err := Rebuild(dir, disk, 1_700_000_000_000)
	if err != nil {
		t.Fatal(err)
	}
	if result.SubjectCount != 3 || result.VerdictCount != 12 || result.EventCount < 13 {
		t.Fatalf("result=%+v", result)
	}
	proj, err := LoadProjection(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := proj.Verdicts[VerdictKey("openai-apikey/gpt-5.4", "codex")]
	if got.Verdict != "VERIFIED" || got.EvidenceLayer != "protocol_conformance" || got.EventID == "" {
		t.Fatalf("verdict=%+v", got)
	}
	body, err := os.ReadFile(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	if strings.Contains(text, "sk-") || strings.Contains(text, `Users`) {
		t.Fatalf("ledger leaked secrets or home path")
	}
}

func TestSubjectsSkipComboAndPolicy(t *testing.T) {
	disk, err := decodeDisk([]byte(`{"providers":{"combo":{"adapter":"openai-chat","models":["x"]},"openai-apikey":{"adapter":"openai-chat","models":["m"]}}}`))
	if err != nil {
		t.Fatal(err)
	}
	subjects := SubjectsFromConfig(disk)
	if len(subjects) != 1 || subjects[0].ID != "openai-apikey/m" {
		t.Fatalf("subjects=%+v", subjects)
	}
}

func decodeDisk(raw []byte) (config.DiskConfig, error) {
	var root struct {
		Providers map[string]json.RawMessage `json:"providers"`
	}
	if err := json.Unmarshal(raw, &root); err != nil {
		return config.DiskConfig{}, err
	}
	return config.DiskConfig{Providers: root.Providers, Raw: raw}, nil
}
```

The HTTP-absence proof is Step 4: `internal/lab` must not import `net/http`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/lab -count=1`

Expected: FAIL (`Rebuild` undefined).

- [ ] **Step 3: Write minimal implementation**

In `internal/compat/matrix.go` add:

```go
VerdictClaimed = "CLAIMED"
```

`internal/lab/subjects.go`: walk `modeldiscovery.CatalogsFromConfig(disk.Providers, disk.Raw, modeldiscovery.DecodeConfig(disk.Raw))`. For each catalog whose `ID` is not `combo` or `policy` and does not contain `/`, collect unique ids from `Selected`, `Custom`, `Discovered` via `modeldiscovery.CatalogID`. Adapter from `compat.ProviderAdapter`. Sort by ID.

`internal/lab/rebuild.go`:

```go
func Rebuild(dir string, disk config.DiskConfig, now int64) (Result, error) {
	subjects := SubjectsFromConfig(disk)
	store := NewStore(dir)
	if err := store.Append(Event{EventKind: KindRunStarted, RecordedAt: now, ProducerVersion: compat.SpecVersion}); err != nil {
		return Result{}, err
	}
	verdicts := 0
	for _, subject := range subjects {
		for _, harness := range compat.Harnesses {
			verdict := compat.Classify(harness, subject.Adapter, catalog.CapabilityUnknown)
			if err := store.Append(Event{
				EventKind: KindVerdictRecorded, RecordedAt: now, ProducerVersion: compat.SpecVersion,
				SubjectID: subject.ID, EvidenceLayer: "protocol_conformance", SuiteID: harness, Verdict: verdict,
			}); err != nil {
				return Result{}, err
			}
			verdicts++
		}
	}
	events, err := store.All()
	if err != nil {
		return Result{}, err
	}
	return Result{EventCount: len(events), SubjectCount: len(subjects), VerdictCount: verdicts}, nil
}
```

`internal/lab/projection.go`: `LoadProjection` calls `store.All()`. Missing file → empty projection, `CorruptionCount: 0`. `ErrCorrupt` → empty verdicts, `CorruptionCount: 1`. Latest `KindVerdictRecorded` per `VerdictKey` wins. Observations = those latest verdict events, max 200, stable order by subject then suite.

`VerdictKey` = `subjectID + "\x00" + suiteID`.

- [ ] **Step 4: Run the tests and make sure they pass**

Run:

```
go test ./internal/lab ./internal/compat -count=1
```

Then confirm rebuild has no HTTP:

```
go list -f "{{.ImportPath}} {{.Imports}}" ./internal/lab
```

Expected: PASS. `internal/lab` imports must not include `net/http`.

- [ ] **Step 5: Commit**

Skip unless the user asked to commit.

---

### Task 3: `benes lab` CLI

**Files:**
- Create: `cmd/benes/lab.go`
- Modify: `cmd/benes/main.go` (`case "lab"` and `printHelp`)
- Modify: `cmd/benes/main_test.go` (`TestRunLabRefusesWithoutActivatingLab` → usage/exit 2)
- Test: `cmd/benes/lab_test.go`

**Interfaces:**
- Consumes: `deps.resolvePaths`, `deps.loadDiskConfig`, `lab.Rebuild`, `lab.LoadProjection`
- Produces: `runLab(args []string, stdout, stderr io.Writer, deps commandDependencies) int`

- [ ] **Step 1: Write the failing tests**

Replace `TestRunLabRefusesWithoutActivatingLab` in `cmd/benes/main_test.go`:

```go
func TestRunLabWithoutArgsPrintsUsage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"lab"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit=%d", code)
	}
	if !strings.Contains(stderr.String(), "benes: usage: lab help|rebuild|status") {
		t.Fatalf("stderr=%q", stderr.String())
	}
}

func TestRunHelpListsLab(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run(context.Background(), []string{"help"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit=%d", code)
	}
	if !strings.Contains(stdout.String(), "  lab        ") {
		t.Fatalf("stdout=%q", stdout.String())
	}
}
```

`cmd/benes/lab_test.go`:

```go
func TestRunLabRebuildAndStatus(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"providers":{"openai-apikey":{"adapter":"openai-responses","models":["gpt-5.4"]}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	deps := commandDependencies{
		resolvePaths: func(config.PathOptions) (config.Paths, error) {
			return config.Paths{Home: home, Config: configPath}, nil
		},
		loadDiskConfig: config.LoadDiskConfig,
	}
	var stdout, stderr bytes.Buffer
	if code := runLab([]string{"rebuild"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("rebuild exit=%d stderr=%q", code, stderr.String())
	}
	stdout.Reset()
	if code := runLab([]string{"status"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("status exit=%d", code)
	}
	if !strings.Contains(stdout.String(), "verdicts") {
		t.Fatalf("status=%q", stdout.String())
	}
	if _, err := os.Stat(filepath.Join(home, "lab", "events.jsonl")); err != nil {
		t.Fatal(err)
	}
}

func TestRunLabHelpExitsZero(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runLab([]string{"help"}, &stdout, &stderr, defaultCommandDependencies()); code != 0 {
		t.Fatalf("exit=%d stderr=%q", code, stderr.String())
	}
}
```

Use the same `config` import as other CLI tests. Look at `cmd/benes/combo_test.go` for `commandDependencies` stubs if `config.Paths` fields differ — match that struct exactly.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./cmd/benes -count=1 -run "TestRunLab|TestRunHelpListsLab"`

Expected: FAIL (`runLab` undefined and/or help missing `lab`).

- [ ] **Step 3: Write minimal implementation**

`cmd/benes/main.go` `case "lab": return runLab(args[1:], stdout, stderr, deps)`

Help line (keep column alignment with neighbors):

```
  lab        help|rebuild|status
```

`cmd/benes/lab.go`:

```go
func runLab(args []string, stdout, stderr io.Writer, deps commandDependencies) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "benes: usage: lab help|rebuild|status")
		return 2
	}
	switch args[0] {
	case "help":
		fmt.Fprintln(stdout, "benes lab help|rebuild|status")
		fmt.Fprintln(stdout, "  rebuild  Record protocol-conformance verdicts from config.json (no provider HTTP)")
		fmt.Fprintln(stdout, "  status   Print lab store counts")
		return 0
	case "rebuild":
		return runLabRebuild(stdout, stderr, deps)
	case "status":
		return runLabStatus(stdout, stderr, deps)
	default:
		fmt.Fprintln(stderr, "benes: usage: lab help|rebuild|status")
		return 2
	}
}
```

`runLabRebuild`: resolve paths → `loadDiskConfig(paths.Config, 0)` → `lab.Rebuild(filepath.Join(filepath.Dir(paths.Config), "lab"), disk, time.Now().UnixMilli())`. Print `rebuilt subjects=%d verdicts=%d events=%d`. Exit 1 on error (`serveFailure` pattern).

`runLabStatus`: `LoadProjection`. If dir missing, print zeros and exit 0. If `CorruptionCount > 0`, print the error to stderr and still print counts (zeros). Human lines: `subjects\tN`, `verdicts\tN`, `events\tN`.

Do not start the listener. Do not touch Codex files.

- [ ] **Step 4: Run the tests and make sure they pass**

Run: `go test ./cmd/benes -count=1 -run "TestRunLab|TestRunHelpListsLab"`

Expected: PASS

- [ ] **Step 5: Commit**

Skip unless the user asked to commit.

---

### Task 4: Lab GET merge

**Files:**
- Modify: `internal/server/lab_api.go`
- Modify: `internal/server/lab_api_test.go`
- Modify: `internal/server/responses.go` only if a new path needs wiring (prefer keeping `/api/lab/events/` in `serveLabAPI`)

**Interfaces:**
- Consumes: `lab.LoadProjection`, `lab.VerdictKey`, `compat.VerdictClaimed`
- Produces: GET `/api/lab/verdicts` includes `protocol_conformance` rows; GET `/api/lab/observations`; GET `/api/lab/events/:id` as `{event: ...}` matching `parseLabEvent`

- [ ] **Step 1: Write the failing tests** (append to `internal/server/lab_api_test.go`)

Keep `TestLabAPIProjectsCapabilityVerdicts` green: it still requires a `live_route_compatibility` row for `openai-apikey/gpt-5.4`. Also assert a `protocol_conformance` row with `CLAIMED` when no store exists.

```go
func TestLabAPIClaimedProtocolWithoutStore(t *testing.T) {
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
		CatalogModels:  []catalog.Model{{ID: "openai-apikey/gpt-5.4", DisplayName: "GPT-5.4"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/lab/verdicts?layer=protocol_conformance", nil)
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"verdict":"CLAIMED"`) {
		t.Fatalf("body=%s", rr.Body.String())
	}
}

func TestLabAPIOverlaysStoreVerdicts(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"providers":{"openai-apikey":{"adapter":"openai-responses","models":["gpt-5.4"]}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	disk, err := config.LoadDiskConfig(configPath, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lab.Rebuild(filepath.Join(home, "lab"), disk, 11); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
		CatalogModels:  []catalog.Model{{ID: "openai-apikey/gpt-5.4"}},
		ConfigPath:     configPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/lab/verdicts?layer=protocol_conformance&suiteId=codex", nil)
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"verdict":"VERIFIED"`) {
		t.Fatalf("body=%s", rr.Body.String())
	}
	obs := httptest.NewRequest(http.MethodGet, "/api/lab/observations", nil)
	obs.Host = "127.0.0.1"
	obsRR := httptest.NewRecorder()
	h.ServeHTTP(obsRR, obs)
	if obsRR.Code != http.StatusOK || !strings.Contains(obsRR.Body.String(), `"executionMode":"local_classifier"`) {
		t.Fatalf("obs=%s", obsRR.Body.String())
	}
}

func TestLabAPICorruptLedgerKeepsDerivedLiveRoute(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"providers":{"openai-apikey":{"adapter":"openai-chat"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, "lab"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "lab", "events.jsonl"), []byte("nope\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
		CatalogModels:  []catalog.Model{{ID: "openai-apikey/gpt-5.4"}},
		ConfigPath:     configPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	status := httptest.NewRequest(http.MethodGet, "/api/lab/status", nil)
	status.Host = "127.0.0.1"
	statusRR := httptest.NewRecorder()
	h.ServeHTTP(statusRR, status)
	if !strings.Contains(statusRR.Body.String(), `"corruptionCount":1`) {
		t.Fatalf("status=%s", statusRR.Body.String())
	}
	req := httptest.NewRequest(http.MethodGet, "/api/lab/verdicts?layer=live_route_compatibility", nil)
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"evidenceLayer":"live_route_compatibility"`) {
		t.Fatalf("body=%s", rr.Body.String())
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/server -count=1 -run TestLabAPI`

Expected: FAIL on CLAIMED / overlay / observations.

- [ ] **Step 3: Write minimal implementation**

In `labProjection()`:

1. Keep emitting live-route rows from `compat.Classify` for every `h.catalogModels` entry (including combo/policy synthesized ids — unchanged).
2. For concrete subjects only (`provider` not `combo`/`policy`), also emit a protocol-conformance row:
   - default `compat.VerdictClaimed`, empty `contributingEventIds`
   - if `configPath` is set, `LoadProjection(filepath.Join(filepath.Dir(h.configPath), "lab"))`
   - on success, overlay matching store verdict + `contributingEventIds: []string{event.EventID}`
   - on corrupt, leave CLAIMED and set `status.corruptionCount`
3. Status `eventCount` / `observationCount` from the projection (0 if missing).
4. `GET /api/lab/observations`: map each projection observation to the GUI DTO:

```go
map[string]any{
  "eventId": ev.EventID,
  "subjectId": ev.SubjectID,
  "evidenceLayer": "protocol_conformance",
  "suiteId": ev.SuiteID,
  "suiteVersion": "1",
  "suiteManifestDigest": digest,
  "scenarioId": "protocol_conformance/" + ev.SuiteID,
  "scenarioVersion": "1",
  "scenarioManifestDigest": digest,
  "outcome": ev.Verdict,
  "completedAt": ev.RecordedAt,
  "executionMode": "local_classifier",
  "excluded": false,
  "exclusionReason": nil,
}
```

Cap 200. Empty array if no store.

5. `GET /api/lab/events/{id}`: find event, write `{"event": {eventKind, eventId, recordedAt, producer, producerVersion, subjectId, evidenceLayer, suiteId, outcome, excluded:false, exclusionReason:null}}`. 404 if missing. Never include payload JSON that looks like a prompt.

Reuse `labVerdict` with an extra `layer` argument instead of hard-coding `live_route_compatibility`.

Do not change profile compatibility gates (`policy_filter.go`).

- [ ] **Step 4: Run the tests and make sure they pass**

Run: `go test ./internal/server -count=1 -run "TestLabAPI|TestFabricAPI"`

Expected: PASS. Existing fabric tests still pass.

- [ ] **Step 5: Commit**

Skip unless the user asked to commit.

---

### Task 5: Fabric settings API

**Files:**
- Create: `internal/server/fabric_settings_api.go`
- Create: `internal/server/fabric_settings_api_test.go`
- Modify: `internal/server/responses.go` (call `serveFabricSettingsAPI` next to `serveContextProjectionAPI`)
- Modify: `internal/server/catalog_isolation_test.go` (add `/api/fabric-settings` to the loopback path list)

**Interfaces:**
- Consumes: `h.commitConfig`, `config.JSONPath("fabric", "enabled")`, `h.fabricEnabled()`
- Produces: loopback `GET`/`PUT /api/fabric-settings` `{enabled: boolean}`; PUT `{ok:true, enabled}`

- [ ] **Step 1: Write the failing test**

Copy `TestContextProjectionAPIReadsAndWritesMode` shape:

```go
func TestFabricSettingsAPIReadsAndWritesEnabled(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"providers":{"openai-apikey":{"adapter":"openai-chat"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
		ConfigPath:     configPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	get := httptest.NewRequest(http.MethodGet, "/api/fabric-settings", nil)
	get.Host = "127.0.0.1"
	got := httptest.NewRecorder()
	h.ServeHTTP(got, get)
	if got.Code != http.StatusOK || !strings.Contains(got.Body.String(), `"enabled":false`) {
		t.Fatalf("get status=%d body=%s", got.Code, got.Body.String())
	}
	put := httptest.NewRequest(http.MethodPut, "/api/fabric-settings", strings.NewReader(`{"enabled":true}`))
	put.Host = "127.0.0.1"
	put.Header.Set("Content-Type", "application/json")
	putRR := httptest.NewRecorder()
	h.ServeHTTP(putRR, put)
	if putRR.Code != http.StatusOK {
		t.Fatalf("put status=%d body=%s", putRR.Code, putRR.Body.String())
	}
	status := httptest.NewRequest(http.MethodGet, "/api/fabric/status", nil)
	status.Host = "127.0.0.1"
	statusRR := httptest.NewRecorder()
	h.ServeHTTP(statusRR, status)
	if !strings.Contains(statusRR.Body.String(), `"enabled":true`) {
		t.Fatalf("status=%s", statusRR.Body.String())
	}
	tasks := httptest.NewRequest(http.MethodGet, "/api/fabric/tasks", nil)
	tasks.Host = "127.0.0.1"
	tasksRR := httptest.NewRecorder()
	h.ServeHTTP(tasksRR, tasks)
	if tasksRR.Code != http.StatusOK {
		t.Fatalf("tasks status=%d body=%s", tasksRR.Code, tasksRR.Body.String())
	}
}
```

Non-loopback GET must 404 (same pattern as context-projection tests if one exists; otherwise add a Host that is not loopback).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/server -count=1 -run TestFabricSettingsAPI`

Expected: FAIL (route not found).

- [ ] **Step 3: Write minimal implementation**

Mirror `context_projection_api.go`:

```go
func (h *handler) serveFabricSettingsAPI(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/api/fabric-settings" {
		return false
	}
	if !isLoopbackRequestHost(r.Host) {
		return false
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		// HEAD 200 JSON content-type; GET {enabled: h.fabricEnabled()}
	case http.MethodPut:
		// decode {enabled bool}; tx.Set(config.JSONPath("fabric", "enabled"), payload)
		// writeJSON {ok:true, enabled}
	default:
		writeError(w, http.StatusNotFound, "not_found", "route not found")
	}
	return true
}
```

PUT does not start Codex, Claude, or a worker. Enablement is live because `fabricEnabled()` already reloads disk.

Wire in `responses.go` beside `serveContextProjectionAPI`.

Add `"/api/fabric-settings"` to the path slice in `catalog_isolation_test.go`.

- [ ] **Step 4: Run the tests and make sure they pass**

Run: `go test ./internal/server -count=1 -run "TestFabricSettingsAPI|TestFabricAPI|TestCatalogIsolation"`

Expected: PASS

- [ ] **Step 5: Commit**

Skip unless the user asked to commit.

---

### Task 6: Control Fabric switch + i18n

**Files:**
- Modify: `gui/src/pages/control-board-settings.ts`
- Modify: `gui/src/pages/control-board.tsx`
- Modify: `gui/src/i18n/en.ts` (source of `TKey`)
- Modify: every other `gui/src/i18n/{de,fr,ja,ko,ru,tr,zh,zh-TW}.ts`

**Interfaces:**
- Consumes: `GET`/`PUT /api/fabric-settings`
- Produces: Control Request-handling switch after the CPG row; keys listed below

Keys (add identical keys to every locale):

```
"nav.tasks": "Tasks"
"control.fabric": "Agent Fabric"
"control.fabricHint": "Opt-in task log. Off by default. Does not start Codex, Claude, or a worker."
"tasks.title": "Tasks"
"tasks.empty": "No fabric tasks yet."
"tasks.offTitle": "Fabric is off"
"tasks.offHint": "Enable Agent Fabric on Control to create tasks."
"tasks.offAction": "Open Control"
"tasks.create": "Create"
"tasks.titleField": "Title"
"tasks.goal": "Goal"
"tasks.state": "State"
"tasks.timeline": "Timeline"
"tasks.start": "Start"
"tasks.close": "Close"
"tasks.remove": "Remove"
"tasks.created": "Task created."
"tasks.startOk": "Run started."
"tasks.closeOk": "Run closed."
"tasks.removeOk": "Task removed."
"tasks.denied": "Fabric request failed."
```

German `de.ts` may translate; other locales must have the same keys (English fallback is acceptable for this slice except `en` and `de`).

- [ ] **Step 1: Write the failing contract**

There is no GUI unit test harness required. The failing check is i18n:

Run: `cd gui && npm run lint:i18n`

After adding keys only to `en.ts`, this fails. Add keys to `en.ts` first, run lint, then fill locales.

- [ ] **Step 2: Confirm lint fails if locales lag**

Run: `cd gui && npm run lint:i18n`

Expected: missing keys in other locale files.

- [ ] **Step 3: Wire Control**

In `control-board-settings.ts`:

- Extend `ControlExtras` / `ControlOverlay` with `fabric: { enabled: boolean } | null`
- Fetch `GET /api/fabric-settings` in `fetchControlExtras` (Promise.all)
- `saveFabric(enabled: boolean)` PUT `{enabled}` then overlay, same try/catch rollback as shadow

In `control-board.tsx` `ControlRequestColumn`, after `ControlContextProjectionRow`, add a `ControlRow` with `Switch` bound to `extras.fabric?.enabled`, `disabled={!extras.fabric || extras.fabricSaving}`.

Do not show a Tasks nav item yet (Task 7). Do not claim models changed.

- [ ] **Step 4: Run GUI checks**

```
cd gui
npm run lint:i18n
npm run lint
npm run build
```

Expected: all three pass. (On Windows this session; do not claim macOS/Linux GUI lint.)

- [ ] **Step 5: Commit**

Skip unless the user asked to commit.

---

### Task 7: Tasks board and nav gate

**Files:**
- Create: `gui/src/pages/Tasks.tsx`
- Create: `gui/src/pages/tasks-api.ts` (fetch helpers + DTO parse)
- Modify: `gui/src/app-routing.ts` (`Page` + `VALID_PAGES`)
- Modify: `gui/src/app-page.tsx`
- Modify: `gui/src/app-shell.ts` (`PAGE_TKEY.tasks = "nav.tasks"`)
- Modify: `gui/src/app-sidebar.tsx`
- Modify: `gui/src/App.tsx` (pass `fabricEnabled`)
- Modify: `gui/src/use-nav-board-prefetch.ts` (prefetch tasks only when enabled)
- Modify: `gui/src/nav-board-resources.ts` (`fabricTasksResourceKey`)
- Modify: `internal/sidecar/fabric/projection.go` (json tags on `TaskDetail` and `Projection` so GET is camelCase)

**Interfaces:**
- Consumes: `GET /api/fabric/status`, `GET/POST /api/fabric/tasks`, `GET /api/fabric/tasks/:id`, `POST .../start`, `POST .../close`, `DELETE .../:id`
- Produces: `#tasks` page; Observation nav item only when enabled

JSON after tags:

```go
type TaskDetail struct {
	Summary    TaskSummary     `json:"summary"`
	Projection Projection      `json:"projection"`
	Timeline   []TimelineEntry `json:"timeline"`
	Events     []Event         `json:"events,omitempty"`
}
```

Add matching `json` tags on `Projection` public fields (`taskId`, `title`, `goal`, `currentOwner`, `fencingToken`, `terminal`, `removed`). Keep kernel tests passing.

- [ ] **Step 1: Write the failing Go test for camelCase GET**

In `internal/server/fabric_api_test.go` after create+start, GET the task and require `"summary"` and `"timeline"` keys (not `"Summary"`). This fails until tags exist.

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/server -count=1 -run TestFabricAPICreateStartCloseRemove`

Expected: FAIL on `"summary"` if you add that assertion first.

- [ ] **Step 3: Implement tags + dashboard**

`tasks-api.ts` parse:

```ts
export type FabricTaskSummary = {
  taskId: string;
  title: string;
  taskState: string;
  runState: string;
  currentOwner: string;
  latestActivity: number;
  eventCount: number;
  terminal: boolean;
};

export type FabricTimelineEntry = {
  sequence: number;
  eventType: string;
  occurredAt: number;
  actorId: string;
  category: string;
};
```

Reject unknown objects; do not use `Reflect.get`.

`Tasks.tsx`:

- `GET /api/fabric/status`. If `enabled !== true`, render divider-only empty board: title `tasks.offTitle`, hint `tasks.offHint`, text link `tasks.offAction` that navigates to Control (`navigateToPage("startup")` / hash `#startup`). No cards.
- If enabled: page-head title + Create (`providers-link providers-link--plain`, 4px inputs). List on the left (or stacked divider sections), selected detail: title, goal, state, timeline. Start / Close / Remove as `providers-link providers-link--plain` (no trailing arrow). `ToastNotice` on every API result.
- Empty list: `tasks.empty`.
- Prefetch: `useDataSurface(fabricTasksResourceKey(apiBase), ...)` only from a hook called when `fabricEnabled` is true. `useNavBoardPrefetch` must fetch status first or accept `enabled` from App; **do not** hit `/api/fabric/tasks` while disabled (404).

Sidebar: `AppNavGroups` takes `fabricEnabled: boolean`. Observation items = sessions, usage, logs, and `tasks` only if true. Not a grey disabled row.

`App.tsx`: fetch `/api/fabric/status` once (and after returning from Control — refetch on hashchange is enough). Pass boolean down.

Do not send `/v1/responses`. Cancel stays API-only.

- [ ] **Step 4: Run checks**

```
go test ./internal/sidecar/fabric ./internal/server -count=1 -run "TestFabric"
cd gui
npm run lint:i18n
npm run lint
npm run build
```

Browser (required for this GUI task): with Vite on **23200** if `benes dev` is already serving it, open `#startup`, enable Fabric, confirm Tasks appears under Observation, create/start/close a task, disable Fabric, confirm nav item disappears and `#tasks` shows the off board. If 23200 is not up, say so and do not recycle production **23100** via `benes stop`.

- [ ] **Step 5: Commit**

Skip unless the user asked to commit.

---

### Task 8: Docs and AGENTS.md

**Files:**
- Modify: `docs/src/content/docs/start/how-traffic-flows.md`
- Modify: `docs/src/content/docs/use/dashboard.md`
- Modify: `docs/src/content/docs/use/sidecars.md`
- Modify: `docs/src/content/docs/reference/http.md`
- Modify: `docs/src/content/docs/reference/config.md`
- Modify: `docs/src/content/docs/reference/packages.md`
- Modify: `AGENTS.md` Learned Workspace Facts (last bullet)

**Interfaces:** none. Claims must match Tasks 1–7.

- [ ] **Step 1: Write the copy**

`how-traffic-flows.md` replace the Lab/Fabric paragraphs with:

- `benes start` still executes no Lab code and does not enable Fabric.
- `benes lab rebuild` writes `~/.benes/lab/events.jsonl` from `config.json` (classifier only, no provider HTTP). `benes lab` without a subcommand exits 2 with usage.
- Compatibility protocol column is `CLAIMED` until a rebuild; after rebuild it shows stored local-run verdicts. Live-route column stays derived adapter+vision. Community stays empty. Profile gates still use `compat.Classify`, not Lab disk.
- Fabric: Control toggle `GET`/`PUT /api/fabric-settings`. Tasks nav (`#tasks`) only when `fabric.enabled`. Start/close are kernel events, not model calls.

`dashboard.md`: Compatibility protocol vs derived live-route; Tasks under Observation when Fabric is on.

`http.md`: Lab GET merge + observations/events; `/api/fabric-settings`.

`config.md`: keep fabric.enabled; mention Control toggle is live (no recycle).

`packages.md`: add `internal/lab`.

`AGENTS.md` last Learned Workspace Facts sentence currently says Fabric has no Tasks nav and `benes lab` exits 2. Replace with: Lab store + `benes lab rebuild`; Fabric Tasks nav gated on `fabric.enabled`; CPG unchanged.

Do not add locale trees under `docs/`.

- [ ] **Step 2: Build docs**

```
cd docs
npm ci
npm run build
```

Expected: success.

- [ ] **Step 3: Privacy scan if ledger/CLI/API strings changed**

Run: `npm run privacy:scan`

Expected: PASS

- [ ] **Step 4: Commit**

Skip unless the user asked to commit.

---

## Spec coverage

| Spec requirement | Task |
| --- | --- |
| JSONL ledger, hash chain, no fabric.Repo | 1 |
| Subjects from config; no combo/policy; no HTTP | 2 |
| `benes lab help\|rebuild\|status`; help lists lab | 3 |
| GET CLAIMED / overlay / observations / events / corrupt | 4 |
| Profile gates unchanged | 4 (explicit non-edit) |
| `/api/fabric-settings`; live enablement | 5 |
| Control switch after CPG | 6 |
| `#tasks`, nav omit, off board, toasts, no worker | 7 |
| Docs + AGENTS.md | 8 |
| Community empty | 4 (leave community handler) |
| No CPG / round-robin / live probes | Global constraints |

## Out of this plan

Live probes (CL-03), task-effectiveness (CL-07), community ingest, Fabric dispatch through `policy/<id>`, Codex JSON-RPC, Claude SDK, A2A, combo round-robin, CPG changes, Lab on `benes start`.
