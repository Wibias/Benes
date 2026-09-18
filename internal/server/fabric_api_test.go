package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFabricAPIDisabledByDefault(t *testing.T) {
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
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodGet, "/api/fabric/status", nil)
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"enabled":false`) {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	tasks := httptest.NewRequest(http.MethodGet, "/api/fabric/tasks", nil)
	tasks.Host = "127.0.0.1"
	tasksRR := httptest.NewRecorder()
	h.ServeHTTP(tasksRR, tasks)
	if tasksRR.Code != http.StatusConflict || !strings.Contains(tasksRR.Body.String(), `"code":"fabric_disabled"`) {
		t.Fatalf("disabled tasks status=%d body=%s", tasksRR.Code, tasksRR.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"kind":"lifecycle"`) {
		t.Fatalf("status missing kind: %s", rr.Body.String())
	}
}

func TestFabricAPICreateStartCloseRemove(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"fabric":{"enabled":true},"providers":{"openai-apikey":{"adapter":"openai-chat"}}}`), 0o600); err != nil {
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
	h = attachHandlerClose(t, h)
	create := httptest.NewRequest(http.MethodPost, "/api/fabric/tasks", strings.NewReader(`{"title":"board","goal":"ship"}`))
	create.Host = "127.0.0.1"
	create.Header.Set("Content-Type", "application/json")
	createRR := httptest.NewRecorder()
	h.ServeHTTP(createRR, create)
	if createRR.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", createRR.Code, createRR.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(createRR.Body.Bytes(), &created); err != nil || created.ID == "" {
		t.Fatalf("created=%s err=%v", createRR.Body.String(), err)
	}
	start := httptest.NewRequest(http.MethodPost, "/api/fabric/tasks/"+created.ID+"/start", strings.NewReader(`{"owner":"operator"}`))
	start.Host = "127.0.0.1"
	startRR := httptest.NewRecorder()
	h.ServeHTTP(startRR, start)
	if startRR.Code != http.StatusOK {
		t.Fatalf("start status=%d body=%s", startRR.Code, startRR.Body.String())
	}
	get := httptest.NewRequest(http.MethodGet, "/api/fabric/tasks/"+created.ID, nil)
	get.Host = "127.0.0.1"
	getRR := httptest.NewRecorder()
	h.ServeHTTP(getRR, get)
	if getRR.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", getRR.Code, getRR.Body.String())
	}
	got := getRR.Body.String()
	if !strings.Contains(got, `"summary"`) {
		t.Fatalf("GET task JSON missing camelCase summary: %s", got)
	}
	if !strings.Contains(got, `"timeline"`) {
		t.Fatalf("GET task JSON missing camelCase timeline: %s", got)
	}
	closeReq := httptest.NewRequest(http.MethodPost, "/api/fabric/tasks/"+created.ID+"/close", strings.NewReader(`{"owner":"operator"}`))
	closeReq.Host = "127.0.0.1"
	closeRR := httptest.NewRecorder()
	h.ServeHTTP(closeRR, closeReq)
	if closeRR.Code != http.StatusOK {
		t.Fatalf("close status=%d body=%s", closeRR.Code, closeRR.Body.String())
	}
	closedGet := httptest.NewRequest(http.MethodGet, "/api/fabric/tasks/"+created.ID, nil)
	closedGet.Host = "127.0.0.1"
	closedRR := httptest.NewRecorder()
	h.ServeHTTP(closedRR, closedGet)
	if closedRR.Code != http.StatusOK || !strings.Contains(closedRR.Body.String(), `"taskState":"completed"`) || !strings.Contains(closedRR.Body.String(), `"runState":"completed"`) {
		t.Fatalf("closed get=%d %s", closedRR.Code, closedRR.Body.String())
	}
	del := httptest.NewRequest(http.MethodDelete, "/api/fabric/tasks/"+created.ID, nil)
	del.Host = "127.0.0.1"
	delRR := httptest.NewRecorder()
	h.ServeHTTP(delRR, del)
	if delRR.Code != http.StatusOK || !strings.Contains(delRR.Body.String(), `"success":true`) {
		t.Fatalf("delete status=%d body=%s", delRR.Code, delRR.Body.String())
	}
}

func TestFabricAPIDisabledIsConsistent(t *testing.T) {
	h, _ := newFabricTestHandler(t, false)
	for _, spec := range []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/api/fabric/tasks", ""},
		{http.MethodPost, "/api/fabric/tasks", `{"title":"x"}`},
		{http.MethodPost, "/api/fabric/tasks/task_1/start", `{"owner":"operator"}`},
		{http.MethodPost, "/api/fabric/tasks/task_1/close", `{"owner":"operator"}`},
		{http.MethodDelete, "/api/fabric/tasks/task_1", ""},
	} {
		rr := fabricDo(h, spec.method, spec.path, spec.body)
		if rr.Code != http.StatusConflict || !strings.Contains(rr.Body.String(), `"code":"fabric_disabled"`) {
			t.Fatalf("%s %s status=%d body=%s", spec.method, spec.path, rr.Code, rr.Body.String())
		}
	}
}

func TestFabricAPILifecycleConflicts(t *testing.T) {
	h, home := newFabricTestHandler(t, true)
	create := fabricDo(h, http.MethodPost, "/api/fabric/tasks", `{"title":"board","goal":"ship"}`)
	if create.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", create.Code, create.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(create.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	closeFirst := fabricDo(h, http.MethodPost, "/api/fabric/tasks/"+created.ID+"/close", `{"owner":"operator"}`)
	if closeFirst.Code != http.StatusConflict || !strings.Contains(closeFirst.Body.String(), `"code":"invalid_transition"`) {
		t.Fatalf("create-close status=%d body=%s", closeFirst.Code, closeFirst.Body.String())
	}
	start := fabricDo(h, http.MethodPost, "/api/fabric/tasks/"+created.ID+"/start", `{"owner":"operator"}`)
	if start.Code != http.StatusOK {
		t.Fatalf("start status=%d body=%s", start.Code, start.Body.String())
	}
	startAgain := fabricDo(h, http.MethodPost, "/api/fabric/tasks/"+created.ID+"/start", `{"owner":"operator"}`)
	if startAgain.Code != http.StatusConflict || !strings.Contains(startAgain.Body.String(), `"code":"invalid_transition"`) {
		t.Fatalf("start-again status=%d body=%s", startAgain.Code, startAgain.Body.String())
	}
	delRunning := fabricDo(h, http.MethodDelete, "/api/fabric/tasks/"+created.ID, "")
	if delRunning.Code != http.StatusConflict || !strings.Contains(delRunning.Body.String(), `"code":"invalid_transition"`) {
		t.Fatalf("delete-running status=%d body=%s", delRunning.Code, delRunning.Body.String())
	}
	closeReq := fabricDo(h, http.MethodPost, "/api/fabric/tasks/"+created.ID+"/close", `{"owner":"operator"}`)
	if closeReq.Code != http.StatusOK {
		t.Fatalf("close status=%d body=%s", closeReq.Code, closeReq.Body.String())
	}
	closeAgain := fabricDo(h, http.MethodPost, "/api/fabric/tasks/"+created.ID+"/close", `{"owner":"operator"}`)
	if closeAgain.Code != http.StatusConflict {
		t.Fatalf("close-again status=%d body=%s", closeAgain.Code, closeAgain.Body.String())
	}
	startAfterClose := fabricDo(h, http.MethodPost, "/api/fabric/tasks/"+created.ID+"/start", `{"owner":"operator"}`)
	if startAfterClose.Code != http.StatusConflict {
		t.Fatalf("start-after-close status=%d body=%s", startAfterClose.Code, startAfterClose.Body.String())
	}
	missing := fabricDo(h, http.MethodGet, "/api/fabric/tasks/task_missing_zzzz", "")
	if missing.Code != http.StatusNotFound || !strings.Contains(missing.Body.String(), `"code":"task_not_found"`) {
		t.Fatalf("missing status=%d body=%s", missing.Code, missing.Body.String())
	}
	badID := fabricDo(h, http.MethodGet, "/api/fabric/tasks/bad.id", "")
	if badID.Code != http.StatusBadRequest || !strings.Contains(badID.Body.String(), `"code":"invalid_task"`) {
		t.Fatalf("bad id status=%d body=%s", badID.Code, badID.Body.String())
	}
	oversized := fabricDo(h, http.MethodPost, "/api/fabric/tasks", `{"title":"`+strings.Repeat("a", 200)+`"}`)
	if oversized.Code != http.StatusBadRequest || !strings.Contains(oversized.Body.String(), `"code":"invalid_task"`) {
		t.Fatalf("oversized status=%d body=%s", oversized.Code, oversized.Body.String())
	}
	trailing := fabricDo(h, http.MethodPost, "/api/fabric/tasks", `{"title":"x"}{"title":"y"}`)
	if trailing.Code != http.StatusBadRequest || !strings.Contains(trailing.Body.String(), `"code":"invalid_body"`) {
		t.Fatalf("trailing status=%d body=%s", trailing.Code, trailing.Body.String())
	}
	tooLarge := fabricDo(h, http.MethodPost, "/api/fabric/tasks", `{"title":"`+strings.Repeat("a", 20<<10)+`"}`)
	if tooLarge.Code != http.StatusBadRequest || !strings.Contains(tooLarge.Body.String(), `"code":"invalid_body"`) {
		t.Fatalf("too large status=%d body=%s", tooLarge.Code, tooLarge.Body.String())
	}
	for _, body := range []string{create.Body.String(), oversized.Body.String(), trailing.Body.String(), tooLarge.Body.String(), missing.Body.String()} {
		if strings.Contains(body, "sk-") || strings.Contains(body, home) {
			t.Fatalf("secret/path leak: %s", body)
		}
	}
}

func TestFabricAPIRejectsNonLoopback(t *testing.T) {
	h, _ := newFabricTestHandler(t, true)
	req := httptest.NewRequest(http.MethodGet, "/api/fabric/tasks", nil)
	req.Header.Set("Authorization", "Bearer local-secret")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("non-loopback status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestFabricEnableDoesNotCreateStore(t *testing.T) {
	h, home := newFabricTestHandler(t, false)
	put := fabricDo(h, http.MethodPut, "/api/fabric-settings", `{"enabled":true}`)
	if put.Code != http.StatusOK {
		t.Fatalf("put status=%d body=%s", put.Code, put.Body.String())
	}
	if _, err := os.Stat(filepath.Join(home, "fabric")); !os.IsNotExist(err) {
		t.Fatalf("enable created fabric dir: %v", err)
	}
	status := fabricDo(h, http.MethodGet, "/api/fabric/status", "")
	if !strings.Contains(status.Body.String(), `"enabled":true`) {
		t.Fatalf("status=%s", status.Body.String())
	}
	list := fabricDo(h, http.MethodGet, "/api/fabric/tasks", "")
	if list.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", list.Code, list.Body.String())
	}
	if _, err := os.Stat(filepath.Join(home, "fabric")); !os.IsNotExist(err) {
		t.Fatalf("list created fabric dir: %v", err)
	}
}

func TestFabricAPIRemovedTaskMutations(t *testing.T) {
	h, _ := newFabricTestHandler(t, true)
	create := fabricDo(h, http.MethodPost, "/api/fabric/tasks", `{"title":"gone"}`)
	if create.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", create.Code, create.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(create.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	del := fabricDo(h, http.MethodDelete, "/api/fabric/tasks/"+created.ID, "")
	if del.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", del.Code, del.Body.String())
	}
	before := fabricDo(h, http.MethodGet, "/api/fabric/tasks/"+created.ID, "")
	if before.Code != http.StatusOK {
		t.Fatalf("get removed status=%d body=%s", before.Code, before.Body.String())
	}
	for _, spec := range []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodPost, "/api/fabric/tasks/" + created.ID + "/start", `{"owner":"operator"}`},
		{http.MethodPost, "/api/fabric/tasks/" + created.ID + "/close", `{"owner":"operator"}`},
		{http.MethodPost, "/api/fabric/tasks/" + created.ID + "/cancel", `{}`},
	} {
		rr := fabricDo(h, spec.method, spec.path, spec.body)
		if rr.Code != http.StatusConflict || !strings.Contains(rr.Body.String(), `"code":"invalid_transition"`) {
			t.Fatalf("%s %s status=%d body=%s", spec.method, spec.path, rr.Code, rr.Body.String())
		}
		if strings.Contains(rr.Body.String(), "fabric_unreadable") {
			t.Fatalf("removed task became generic 500: %s", rr.Body.String())
		}
	}
	again := fabricDo(h, http.MethodDelete, "/api/fabric/tasks/"+created.ID, "")
	if again.Code != http.StatusOK {
		t.Fatalf("repeat delete status=%d body=%s", again.Code, again.Body.String())
	}
	after := fabricDo(h, http.MethodGet, "/api/fabric/tasks/"+created.ID, "")
	if after.Code != http.StatusOK || after.Body.String() != before.Body.String() {
		t.Fatalf("removed task mutated\nbefore=%s\nafter=%s", before.Body.String(), after.Body.String())
	}
}

func TestFabricAPICancelJSON(t *testing.T) {
	h, _ := newFabricTestHandler(t, true)
	create := fabricDo(h, http.MethodPost, "/api/fabric/tasks", `{"title":"cancel"}`)
	if create.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", create.Code, create.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(create.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	path := "/api/fabric/tasks/" + created.ID + "/cancel"
	baseline := fabricDo(h, http.MethodGet, "/api/fabric/tasks/"+created.ID, "")
	for _, body := range []string{`{`, `{"principal":"operator"}{"principal":"x"}`, `{"principal":"operator","worker":1}`, `{"principal":"` + strings.Repeat("a", 70<<10) + `"}`} {
		rr := fabricDo(h, http.MethodPost, path, body)
		if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), `"code":"invalid_body"`) {
			t.Fatalf("invalid cancel status=%d body=%s", rr.Code, rr.Body.String())
		}
		got := fabricDo(h, http.MethodGet, "/api/fabric/tasks/"+created.ID, "")
		if got.Body.String() != baseline.Body.String() {
			t.Fatalf("invalid cancel mutated task\n%s\n%s", baseline.Body.String(), got.Body.String())
		}
	}
	empty := fabricDo(h, http.MethodPost, path, "")
	if empty.Code != http.StatusOK {
		t.Fatalf("empty cancel status=%d body=%s", empty.Code, empty.Body.String())
	}
	id2Create := fabricDo(h, http.MethodPost, "/api/fabric/tasks", `{"title":"cancel2"}`)
	var id2 struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(id2Create.Body.Bytes(), &id2); err != nil {
		t.Fatal(err)
	}
	valid := fabricDo(h, http.MethodPost, "/api/fabric/tasks/"+id2.ID+"/cancel", `{"principal":"operator"}`)
	if valid.Code != http.StatusOK {
		t.Fatalf("valid cancel status=%d body=%s", valid.Code, valid.Body.String())
	}
}

func TestFabricAPICorruptConfigIsNotDisabled(t *testing.T) {
	h, home := newFabricTestHandler(t, true)
	if err := os.WriteFile(filepath.Join(home, "config.json"), []byte(`{`), 0o600); err != nil {
		t.Fatal(err)
	}
	status := fabricDo(h, http.MethodGet, "/api/fabric/status", "")
	if status.Code != http.StatusInternalServerError || !strings.Contains(status.Body.String(), `"code":"config_unreadable"`) {
		t.Fatalf("status=%d body=%s", status.Code, status.Body.String())
	}
	if strings.Contains(status.Body.String(), `"enabled":false`) || strings.Contains(status.Body.String(), `"code":"fabric_disabled"`) {
		t.Fatalf("corrupt config masqueraded as disabled: %s", status.Body.String())
	}
	if strings.Contains(status.Body.String(), home) {
		t.Fatalf("path leak: %s", status.Body.String())
	}
	tasks := fabricDo(h, http.MethodGet, "/api/fabric/tasks", "")
	if tasks.Code != http.StatusInternalServerError || !strings.Contains(tasks.Body.String(), `"code":"config_unreadable"`) {
		t.Fatalf("tasks=%d body=%s", tasks.Code, tasks.Body.String())
	}
	settings := fabricDo(h, http.MethodGet, "/api/fabric-settings", "")
	if settings.Code != http.StatusInternalServerError || !strings.Contains(settings.Body.String(), `"code":"config_unreadable"`) {
		t.Fatalf("settings=%d body=%s", settings.Code, settings.Body.String())
	}
}

func TestFabricAPIAuditOwnerIsNotFencingAuthority(t *testing.T) {
	h, _ := newFabricTestHandler(t, true)
	create := fabricDo(h, http.MethodPost, "/api/fabric/tasks", `{"title":"audit"}`)
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(create.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	start := fabricDo(h, http.MethodPost, "/api/fabric/tasks/"+created.ID+"/start", `{"owner":"alice"}`)
	if start.Code != http.StatusOK {
		t.Fatalf("start=%d %s", start.Code, start.Body.String())
	}
	got := fabricDo(h, http.MethodGet, "/api/fabric/tasks/"+created.ID, "")
	if got.Code != http.StatusOK {
		t.Fatal(got.Body.String())
	}
	if !strings.Contains(got.Body.String(), `"currentOwner":""`) {
		t.Fatalf("owner seeded fencing: %s", got.Body.String())
	}
	if !strings.Contains(got.Body.String(), `"actorId":"alice"`) {
		t.Fatalf("missing audit actor: %s", got.Body.String())
	}
	closeReq := fabricDo(h, http.MethodPost, "/api/fabric/tasks/"+created.ID+"/close", `{"owner":"bob"}`)
	if closeReq.Code != http.StatusOK {
		t.Fatalf("close=%d %s", closeReq.Code, closeReq.Body.String())
	}
	closed := fabricDo(h, http.MethodGet, "/api/fabric/tasks/"+created.ID, "")
	if !strings.Contains(closed.Body.String(), `"currentOwner":""`) || !strings.Contains(closed.Body.String(), `"taskState":"completed"`) {
		t.Fatalf("close fencing/state: %s", closed.Body.String())
	}
}

func TestFabricAPIRejectsSecretOwnerAndPrincipal(t *testing.T) {
	h, home := newFabricTestHandler(t, true)
	create := fabricDo(h, http.MethodPost, "/api/fabric/tasks", `{"title":"priv"}`)
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(create.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	secret := "sk-abcdefghijklmnopqrstuvwxyz012345"
	start := fabricDo(h, http.MethodPost, "/api/fabric/tasks/"+created.ID+"/start", `{"owner":"`+secret+`"}`)
	if start.Code != http.StatusBadRequest || !strings.Contains(start.Body.String(), `"code":"invalid_task"`) {
		t.Fatalf("secret owner start=%d %s", start.Code, start.Body.String())
	}
	if strings.Contains(start.Body.String(), secret) {
		t.Fatalf("secret leaked in error: %s", start.Body.String())
	}
	homePath := `C:\\Users\\alice\\bin`
	homeStart := fabricDo(h, http.MethodPost, "/api/fabric/tasks/"+created.ID+"/start", `{"owner":"`+homePath+`"}`)
	if homeStart.Code != http.StatusBadRequest {
		t.Fatalf("home owner start=%d %s", homeStart.Code, homeStart.Body.String())
	}
	cancel := fabricDo(h, http.MethodPost, "/api/fabric/tasks/"+created.ID+"/cancel", `{"principal":"`+secret+`"}`)
	if cancel.Code != http.StatusBadRequest || strings.Contains(cancel.Body.String(), secret) {
		t.Fatalf("secret principal=%d %s", cancel.Code, cancel.Body.String())
	}
	got := fabricDo(h, http.MethodGet, "/api/fabric/tasks/"+created.ID, "")
	if strings.Contains(got.Body.String(), secret) || strings.Contains(got.Body.String(), "alice") {
		t.Fatalf("secret in detail: %s", got.Body.String())
	}
	raw, err := os.ReadFile(filepath.Join(home, "fabric", created.ID+".events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), secret) || strings.Contains(string(raw), `Users\alice`) {
		t.Fatalf("secret in jsonl: %s", raw)
	}
}

func TestFabricAPICancelPreconditions(t *testing.T) {
	h, _ := newFabricTestHandler(t, true)
	create := fabricDo(h, http.MethodPost, "/api/fabric/tasks", `{"title":"pre"}`)
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(create.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	path := "/api/fabric/tasks/" + created.ID + "/cancel"
	baseline := fabricDo(h, http.MethodGet, "/api/fabric/tasks/"+created.ID, "")
	tokenOnly := fabricDo(h, http.MethodPost, path, `{"expectedFencingToken":123}`)
	if tokenOnly.Code != http.StatusBadRequest || !strings.Contains(tokenOnly.Body.String(), `"code":"invalid_body"`) {
		t.Fatalf("token only=%d %s", tokenOnly.Code, tokenOnly.Body.String())
	}
	ownerOnly := fabricDo(h, http.MethodPost, path, `{"expectedOwner":"rs_a"}`)
	if ownerOnly.Code != http.StatusBadRequest || !strings.Contains(ownerOnly.Body.String(), `"code":"invalid_body"`) {
		t.Fatalf("owner only=%d %s", ownerOnly.Code, ownerOnly.Body.String())
	}
	staleBoth := fabricDo(h, http.MethodPost, path, `{"expectedOwner":"stale","expectedFencingToken":9}`)
	if staleBoth.Code != http.StatusConflict || !strings.Contains(staleBoth.Body.String(), `"code":"invalid_transition"`) {
		t.Fatalf("stale both=%d %s", staleBoth.Code, staleBoth.Body.String())
	}
	after := fabricDo(h, http.MethodGet, "/api/fabric/tasks/"+created.ID, "")
	if after.Body.String() != baseline.Body.String() {
		t.Fatalf("precondition cancel mutated\n%s\n%s", baseline.Body.String(), after.Body.String())
	}
	okBoth := fabricDo(h, http.MethodPost, path, `{"expectedOwner":"","expectedFencingToken":0}`)
	if okBoth.Code != http.StatusOK {
		t.Fatalf("both current=%d %s", okBoth.Code, okBoth.Body.String())
	}
	id2 := fabricDo(h, http.MethodPost, "/api/fabric/tasks", `{"title":"pre2"}`)
	var created2 struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(id2.Body.Bytes(), &created2); err != nil {
		t.Fatal(err)
	}
	empty := fabricDo(h, http.MethodPost, "/api/fabric/tasks/"+created2.ID+"/cancel", "")
	if empty.Code != http.StatusOK {
		t.Fatalf("default cancel=%d %s", empty.Code, empty.Body.String())
	}
}

func newFabricTestHandler(t *testing.T, enabled bool) (http.Handler, string) {
	t.Helper()
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	raw := `{"providers":{"openai-apikey":{"adapter":"openai-chat"}},"defaultProvider":"openai-apikey"}`
	if enabled {
		raw = `{"fabric":{"enabled":true},"providers":{"openai-apikey":{"adapter":"openai-chat"}},"defaultProvider":"openai-apikey"}`
	}
	if err := os.WriteFile(configPath, []byte(raw), 0o600); err != nil {
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
	h = attachHandlerClose(t, h)
	return h, home
}

func fabricDo(h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}
