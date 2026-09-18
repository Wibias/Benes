package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Wibias/Benes/internal/storage"
)

func newStorageHandler(t *testing.T, home string) http.Handler {
	t.Helper()
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
		CodexHome:      home,
		ConfigPath:     filepath.Join(t.TempDir(), "config.json"),
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	if impl, ok := h.(*handler); ok {
		t.Cleanup(func() {
			deadline := time.Now().Add(2 * time.Second)
			for impl.storageJobRunning() {
				if time.Now().After(deadline) {
					t.Errorf("storage job did not finish before test cleanup")
					return
				}
				time.Sleep(time.Millisecond)
			}
		})
	}
	return h
}

func storageLoopbackJSON(t *testing.T, h http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Host = "127.0.0.1"
	if body != nil {
		req.Header.Set("content-type", "application/json")
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func TestCleanupPreviewExecuteRestoreAPI(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "archived_sessions")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.jsonl"), []byte("aaaa"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filepath.Join(dir, "a.jsonl"), time.Unix(100, 0), time.Unix(100, 0)); err != nil {
		t.Fatal(err)
	}
	h := newStorageHandler(t, home)
	previewRR := storageLoopbackJSON(t, h, http.MethodPost, "/api/storage/cleanup/preview", map[string]any{"percent": 100})
	if previewRR.Code != http.StatusOK {
		t.Fatalf("preview status=%d body=%s", previewRR.Code, previewRR.Body.String())
	}
	var preview struct {
		Digest string `json:"digest"`
		Count  int    `json:"count"`
		Bytes  int64  `json:"bytes"`
	}
	if err := json.Unmarshal(previewRR.Body.Bytes(), &preview); err != nil || preview.Digest == "" || preview.Count != 1 {
		t.Fatalf("preview=%s err=%v", previewRR.Body.String(), err)
	}
	cleanRR := storageLoopbackJSON(t, h, http.MethodPost, "/api/storage/cleanup", map[string]any{
		"percent": 100, "mode": "quarantine", "digest": preview.Digest,
	})
	if cleanRR.Code != http.StatusOK {
		t.Fatalf("cleanup status=%d body=%s", cleanRR.Code, cleanRR.Body.String())
	}
	var cleaned struct {
		OK    bool   `json:"ok"`
		Count int    `json:"count"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(cleanRR.Body.Bytes(), &cleaned); err != nil || !cleaned.OK || cleaned.Count != 1 {
		t.Fatalf("cleanup=%s", cleanRR.Body.String())
	}
	listRR := storageLoopbackJSON(t, h, http.MethodGet, "/api/storage/trash", nil)
	var listed struct {
		Entries []struct {
			ID string `json:"id"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(listRR.Body.Bytes(), &listed); err != nil || len(listed.Entries) != 1 {
		t.Fatalf("trash=%s", listRR.Body.String())
	}
	restoreRR := storageLoopbackJSON(t, h, http.MethodPost, "/api/storage/trash/restore", map[string]any{"id": listed.Entries[0].ID})
	var restored struct {
		OK    bool `json:"ok"`
		Count int  `json:"count"`
	}
	if err := json.Unmarshal(restoreRR.Body.Bytes(), &restored); err != nil || !restored.OK {
		t.Fatalf("restore=%s", restoreRR.Body.String())
	}
}

func TestCleanupAPIStalePreview(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "archived_sessions")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "a.jsonl")
	if err := os.WriteFile(path, []byte("aaaa"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := newStorageHandler(t, home)
	previewRR := storageLoopbackJSON(t, h, http.MethodPost, "/api/storage/cleanup/preview", map[string]any{"percent": 100})
	var preview struct {
		Digest string `json:"digest"`
	}
	_ = json.Unmarshal(previewRR.Body.Bytes(), &preview)
	if err := os.WriteFile(path, []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	cleanRR := storageLoopbackJSON(t, h, http.MethodPost, "/api/storage/cleanup", map[string]any{
		"percent": 100, "mode": "quarantine", "digest": preview.Digest,
	})
	var cleaned struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	_ = json.Unmarshal(cleanRR.Body.Bytes(), &cleaned)
	if cleaned.OK || cleaned.Error != "stale_preview" {
		t.Fatalf("body=%s", cleanRR.Body.String())
	}
}

func TestCleanupPolicyAPIRoundTrip(t *testing.T) {
	home := t.TempDir()
	h := newStorageHandler(t, home)
	getRR := storageLoopbackJSON(t, h, http.MethodGet, "/api/storage/cleanup-policy", nil)
	if getRR.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", getRR.Code, getRR.Body.String())
	}
	putRR := storageLoopbackJSON(t, h, http.MethodPut, "/api/storage/cleanup-policy", map[string]any{
		"enabled":  false,
		"trigger":  map[string]any{"archivedBytesOver": 1024},
		"target":   map[string]any{"removeOldestPercent": 10},
		"schedule": "manual",
		"mode":     "quarantine",
	})
	if putRR.Code != http.StatusOK {
		t.Fatalf("put status=%d body=%s", putRR.Code, putRR.Body.String())
	}
	runRR := storageLoopbackJSON(t, h, http.MethodPost, "/api/storage/cleanup-policy/run", map[string]any{})
	if runRR.Code != http.StatusOK {
		t.Fatalf("run status=%d body=%s", runRR.Code, runRR.Body.String())
	}
	var started struct {
		Started bool `json:"started"`
		Job     struct {
			StartedAt int64 `json:"startedAt"`
		} `json:"job"`
	}
	if err := json.Unmarshal(runRR.Body.Bytes(), &started); err != nil || !started.Started || started.Job.StartedAt == 0 {
		t.Fatalf("run=%s", runRR.Body.String())
	}
}

func TestCleanupPolicyGetDoesNotClearPersistedRunning(t *testing.T) {
	home := t.TempDir()
	raw, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
		CodexHome:      home,
		ConfigPath:     filepath.Join(t.TempDir(), "config.json"),
	})
	if err != nil {
		t.Fatal(err)
	}
	raw = attachHandlerClose(t, raw)
	h := raw.(*handler)
	policy := h.loadCleanupPolicy()
	policy.Job = &storage.PolicyJob{Status: storage.JobRunning, StartedAt: time.Now().UnixMilli(), Reason: "manual"}
	if err := h.saveCleanupPolicy(policy); err != nil {
		t.Fatal(err)
	}
	getRR := storageLoopbackJSON(t, h, http.MethodGet, "/api/storage/cleanup-policy", nil)
	var got storage.Policy
	if err := json.Unmarshal(getRR.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Job == nil || got.Job.Status != storage.JobRunning {
		t.Fatalf("GET cleared running job: %s", getRR.Body.String())
	}
	getRR = storageLoopbackJSON(t, h, http.MethodGet, "/api/storage/cleanup-policy", nil)
	if err := json.Unmarshal(getRR.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Job == nil || got.Job.Status != storage.JobRunning {
		t.Fatalf("second GET cleared running job: %s", getRR.Body.String())
	}
}

func TestCleanupPolicyAlreadyRunningAndPutConflict(t *testing.T) {
	home := t.TempDir()
	raw, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
		CodexHome:      home,
		ConfigPath:     filepath.Join(t.TempDir(), "config.json"),
	})
	if err != nil {
		t.Fatal(err)
	}
	raw = attachHandlerClose(t, raw)
	h := raw.(*handler)
	if !h.beginStorageJob("manual") {
		t.Fatal("begin")
	}
	runRR := storageLoopbackJSON(t, h, http.MethodPost, "/api/storage/cleanup-policy/run", map[string]any{})
	if runRR.Code != http.StatusConflict {
		t.Fatalf("run status=%d body=%s", runRR.Code, runRR.Body.String())
	}
	var runBody struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(runRR.Body.Bytes(), &runBody)
	if runBody.Error != storage.CodeAlreadyRunning {
		t.Fatalf("run=%s", runRR.Body.String())
	}
	putRR := storageLoopbackJSON(t, h, http.MethodPut, "/api/storage/cleanup-policy", map[string]any{
		"enabled":  false,
		"trigger":  map[string]any{"archivedBytesOver": 1024},
		"target":   map[string]any{"removeOldestPercent": 10},
		"schedule": "manual",
		"mode":     "quarantine",
	})
	if putRR.Code != http.StatusConflict {
		t.Fatalf("put status=%d body=%s", putRR.Code, putRR.Body.String())
	}
	getRR := storageLoopbackJSON(t, h, http.MethodGet, "/api/storage/cleanup-policy", nil)
	var got storage.Policy
	if err := json.Unmarshal(getRR.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Job == nil || got.Job.Status != storage.JobRunning {
		t.Fatalf("GET while running=%s", getRR.Body.String())
	}
}

func TestCleanupPolicyRecoverStaleRunningOnlyAtStart(t *testing.T) {
	home := t.TempDir()
	raw, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
		CodexHome:      home,
		ConfigPath:     filepath.Join(t.TempDir(), "config.json"),
	})
	if err != nil {
		t.Fatal(err)
	}
	raw = attachHandlerClose(t, raw)
	h := raw.(*handler)
	policy := h.loadCleanupPolicy()
	policy.Job = &storage.PolicyJob{Status: storage.JobRunning, StartedAt: 1, Reason: "startup"}
	if err := h.saveCleanupPolicy(policy); err != nil {
		t.Fatal(err)
	}
	getRR := storageLoopbackJSON(t, h, http.MethodGet, "/api/storage/cleanup-policy", nil)
	var got storage.Policy
	_ = json.Unmarshal(getRR.Body.Bytes(), &got)
	if got.Job == nil || got.Job.Status != storage.JobRunning {
		t.Fatalf("ordinary GET recovered job: %s", getRR.Body.String())
	}
	h.recoverStorageJob()
	got = h.readCleanupPolicy()
	if got.Job == nil || got.Job.Status != storage.JobIdle {
		t.Fatalf("process-start recovery=%+v", got.Job)
	}
	if got.Job.LastError != storage.CodeRestoreWorkerAborted {
		t.Fatalf("lastError=%q", got.Job.LastError)
	}
}

func TestCleanupPolicyOldFinishDoesNotOverwriteNewerGeneration(t *testing.T) {
	home := t.TempDir()
	raw, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
		CodexHome:      home,
		ConfigPath:     filepath.Join(t.TempDir(), "config.json"),
	})
	if err != nil {
		t.Fatal(err)
	}
	raw = attachHandlerClose(t, raw)
	h := raw.(*handler)
	if !h.beginStorageJob("old") {
		t.Fatal("begin old")
	}
	oldGen, oldStarted := h.currentStorageJob()
	h.endStorageJob(oldGen)
	if !h.beginStorageJob("new") {
		t.Fatal("begin new")
	}
	newGen, _ := h.currentStorageJob()
	if newGen == oldGen {
		t.Fatal("generation did not advance")
	}
	h.finishStorageJob(oldGen, oldStarted, storage.Policy{Job: &storage.PolicyJob{LastError: "stale-old"}})
	if !h.storageJobRunning() {
		t.Fatal("old finish cleared newer job")
	}
	got := h.readCleanupPolicy()
	if got.Job != nil && got.Job.LastError == "stale-old" {
		t.Fatalf("old finish overwrote policy: %+v", got.Job)
	}
}

func TestStorageSchedulerStopEndpointCancelsLoop(t *testing.T) {
	home := t.TempDir()
	raw, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
		CodexHome:      home,
		ConfigPath:     filepath.Join(t.TempDir(), "config.json"),
	})
	if err != nil {
		t.Fatal(err)
	}
	raw = attachHandlerClose(t, raw)
	h := raw.(*handler)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h.StartStorageCleanup(ctx)
	if h.storageCancel == nil {
		t.Fatal("storage cancel missing")
	}
	stopped := make(chan struct{})
	prev := h.stop
	h.stop = func() {
		prev()
		close(stopped)
	}
	h.stop()
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("stop did not run")
	}
}

func TestTrashListAPIReportsRecoveryNeeded(t *testing.T) {
	home := t.TempDir()
	id := "1700000000097-cccccccc"
	dir := filepath.Join(home, ".trash", id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte("{not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := newStorageHandler(t, home)
	listRR := storageLoopbackJSON(t, h, http.MethodGet, "/api/storage/trash", nil)
	if listRR.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", listRR.Code, listRR.Body.String())
	}
	var listed struct {
		Entries        []any `json:"entries"`
		RecoveryNeeded []any `json:"recoveryNeeded"`
	}
	if err := json.Unmarshal(listRR.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Entries) != 0 || len(listed.RecoveryNeeded) == 0 {
		t.Fatalf("trash API hid recovery: %s", listRR.Body.String())
	}
}

func TestDecodeLimitedJSONBodyLimits(t *testing.T) {
	home := t.TempDir()
	h := newStorageHandler(t, home)
	ok := storageLoopbackJSON(t, h, http.MethodPost, "/api/storage/cleanup/preview", map[string]any{"percent": 100})
	if ok.Code != http.StatusOK {
		t.Fatalf("normal=%d body=%s", ok.Code, ok.Body.String())
	}
	near := `{"percent":100` + strings.Repeat(" ", 4000) + `}`
	if int64(len(near)) > 4<<10 {
		near = `{"percent":100` + strings.Repeat(" ", (4<<10)-len(`{"percent":100}`)-1) + `}`
	}
	req := httptest.NewRequest(http.MethodPost, "/api/storage/cleanup/preview", strings.NewReader(near))
	req.Host = "127.0.0.1"
	req.Header.Set("content-type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("near-limit status=%d body=%s", rr.Code, rr.Body.String())
	}
	over := `{"percent":100}` + strings.Repeat(" ", 5000)
	req = httptest.NewRequest(http.MethodPost, "/api/storage/cleanup/preview", strings.NewReader(over))
	req.Host = "127.0.0.1"
	req.Header.Set("content-type", "application/json")
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("whitespace-over-limit status=%d body=%s", rr.Code, rr.Body.String())
	}
	two := `{"percent":100}{"percent":1}`
	req = httptest.NewRequest(http.MethodPost, "/api/storage/cleanup/preview", strings.NewReader(two))
	req.Host = "127.0.0.1"
	req.Header.Set("content-type", "application/json")
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("second json status=%d body=%s", rr.Code, rr.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, "/api/storage/cleanup/preview", strings.NewReader("{not-json"))
	req.Host = "127.0.0.1"
	req.Header.Set("content-type", "application/json")
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("malformed status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestFinishStorageJobPersistFailureDoesNotStayRunning(t *testing.T) {
	home := t.TempDir()
	raw, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
		CodexHome:      home,
		ConfigPath:     filepath.Join(t.TempDir(), "config.json"),
	})
	if err != nil {
		t.Fatal(err)
	}
	raw = attachHandlerClose(t, raw)
	h := raw.(*handler)
	if !h.beginStorageJob("manual") {
		t.Fatal("begin")
	}
	gen, started := h.currentStorageJob()
	running := h.loadCleanupPolicy()
	running.Job = &storage.PolicyJob{Status: storage.JobRunning, StartedAt: started, Reason: "manual", Generation: gen}
	if err := h.saveCleanupPolicy(running); err != nil {
		t.Fatal(err)
	}
	h.storagePersistErr = errors.New("disk full")
	finished := storage.DefaultPolicy()
	finished.Job = &storage.PolicyJob{Status: storage.JobIdle, LastOutcome: &storage.JobOutcome{OK: true}}
	h.finishStorageJob(gen, started, finished)
	h.storagePersistErr = nil
	if h.storageJobRunning() {
		t.Fatal("in-process still running after persist failure")
	}
	got := h.loadCleanupPolicy()
	if got.Job == nil || got.Job.Status == storage.JobRunning {
		t.Fatalf("GET still running after persist failure: %+v", got.Job)
	}
	if got.Job.LastError != "config_write_failed" {
		t.Fatalf("lastError=%q", got.Job.LastError)
	}
}

func TestCleanupPolicyFirstDailyNextRunSurvivesGet(t *testing.T) {
	home := t.TempDir()
	h := newStorageHandler(t, home)
	putRR := storageLoopbackJSON(t, h, http.MethodPut, "/api/storage/cleanup-policy", map[string]any{
		"enabled":  true,
		"trigger":  map[string]any{"archivedBytesOver": 1024},
		"target":   map[string]any{"removeOldestPercent": 10},
		"schedule": "daily",
		"mode":     "quarantine",
	})
	if putRR.Code != http.StatusOK {
		t.Fatalf("put status=%d body=%s", putRR.Code, putRR.Body.String())
	}
	var first struct {
		Policy storage.Policy `json:"policy"`
	}
	if err := json.Unmarshal(putRR.Body.Bytes(), &first); err != nil || first.Policy.NextRun == nil {
		t.Fatalf("put=%s err=%v", putRR.Body.String(), err)
	}
	get1 := storageLoopbackJSON(t, h, http.MethodGet, "/api/storage/cleanup-policy", nil)
	get2 := storageLoopbackJSON(t, h, http.MethodGet, "/api/storage/cleanup-policy", nil)
	var p1, p2 storage.Policy
	_ = json.Unmarshal(get1.Body.Bytes(), &p1)
	_ = json.Unmarshal(get2.Body.Bytes(), &p2)
	if p1.NextRun == nil || p2.NextRun == nil || *p1.NextRun != *first.Policy.NextRun || *p2.NextRun != *first.Policy.NextRun {
		t.Fatalf("GET moved nextRun first=%v get1=%v get2=%v", first.Policy.NextRun, p1.NextRun, p2.NextRun)
	}
}
