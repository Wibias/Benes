package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/catalog"
)

func TestClaudeCodeGETDefaultsAndSupportedFields(t *testing.T) {
	h := claudeCodeContractHandler(t, `{}`, []catalog.Model{{ID: "openai-apikey/gpt-5.5"}, {ID: "gpt-5.6-sol"}})
	got := claudeCodeGET(t, h)
	if got["enabled"] != true || got["authMode"] != "auto" || got["autoContext"] != true || got["injectAgents"] != true {
		t.Fatalf("defaults=%v", got)
	}
	for _, key := range []string{"systemEnv", "fastMode", "webSearchSidecar", "visionSidecar"} {
		if _, ok := got[key]; ok {
			t.Fatalf("retired field %s must not be exposed: %v", key, got[key])
		}
	}
	if got["smallFastModel"] != "" {
		t.Fatalf("empty helper=%v", got["smallFastModel"])
	}
	if got["autoCompactWindow"] != nil {
		t.Fatalf("absent compact window must stay null, got %v", got["autoCompactWindow"])
	}
	aliases, _ := got["aliases"].([]any)
	if aliases == nil || len(aliases) != 0 {
		t.Fatalf("aliases must be an empty read-only list, got %v", got["aliases"])
	}
	available, _ := got["available"].([]any)
	if len(available) != 2 || available[0] != "openai-apikey/gpt-5.5" || available[1] != "gpt-5.6-sol" {
		t.Fatalf("available must be the central catalog, got %v", available)
	}
	modelMap, _ := got["modelMap"].(map[string]any)
	tierModels, _ := got["tierModels"].(map[string]any)
	if len(modelMap) != 0 || len(tierModels) != 0 {
		t.Fatalf("empty families=%v tier=%v", modelMap, tierModels)
	}
}

func TestClaudeCodePUTPersistsSupportedSettingsWithoutCoercingOmittedFields(t *testing.T) {
	configPath := claudeCodeConfigPath(t, `{
		"claudeCode":{
			"enabled":true,
			"authMode":"proxy",
			"autoContext":false,
			"injectAgents":false,
			"smallFastModel":"gpt-5.4-mini",
			"autoCompactWindow":350000,
			"modelMap":{"opus":"gpt-5.6-sol","sonnet":"keep-me"}
		},
		"apiKeys":[{"key":"sk-secret"}]
	}`)
	h := claudeCodeContractHandlerAt(t, configPath, nil)
	rr := claudeCodeDo(t, h, http.MethodPut, `{"authMode":"subscription","autoCompactWindow":250000}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("put status=%d body=%s", rr.Code, rr.Body.String())
	}
	var okBody map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &okBody); err != nil || okBody["ok"] != true {
		t.Fatalf("success body=%s", rr.Body.String())
	}
	got := claudeCodeGET(t, h)
	if got["authMode"] != "subscription" {
		t.Fatalf("authMode=%v", got["authMode"])
	}
	if got["enabled"] != true || got["autoContext"] != false || got["injectAgents"] != false {
		t.Fatalf("omitted fields were coerced: %v", got)
	}
	if got["smallFastModel"] != "gpt-5.4-mini" {
		t.Fatalf("omitted helper was coerced: %v", got["smallFastModel"])
	}
	if compact, _ := got["autoCompactWindow"].(float64); compact != 250000 {
		t.Fatalf("compact=%v", got["autoCompactWindow"])
	}
	modelMap, _ := got["modelMap"].(map[string]any)
	tierModels, _ := got["tierModels"].(map[string]any)
	if modelMap["opus"] != "gpt-5.6-sol" || modelMap["sonnet"] != "keep-me" {
		t.Fatalf("omitted family map was coerced: %v", modelMap)
	}
	if tierModels["opus"] != "gpt-5.6-sol" || tierModels["sonnet"] != "keep-me" {
		t.Fatalf("GET tierModels must project leftover family map: %v", tierModels)
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "sk-secret") {
		t.Fatal("unrelated secrets must remain")
	}
}

func TestClaudeCodePUTDefaultAuthAndTogglesDeleteStoredKeys(t *testing.T) {
	configPath := claudeCodeConfigPath(t, `{"claudeCode":{"authMode":"proxy","autoContext":false,"injectAgents":false,"smallFastModel":"gpt-5.4-mini"}}`)
	h := claudeCodeContractHandlerAt(t, configPath, nil)
	rr := claudeCodeDo(t, h, http.MethodPut, `{"authMode":"auto","autoContext":true,"injectAgents":true,"smallFastModel":""}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("put status=%d body=%s", rr.Code, rr.Body.String())
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, key := range []string{`"authMode"`, `"autoContext"`, `"injectAgents"`, `"smallFastModel"`} {
		if strings.Contains(text, key) {
			t.Fatalf("default %s must be omitted from disk, config=%s", key, text)
		}
	}
	got := claudeCodeGET(t, h)
	if got["authMode"] != "auto" || got["autoContext"] != true || got["injectAgents"] != true || got["smallFastModel"] != "" {
		t.Fatalf("GET must re-synthesize defaults, got %v", got)
	}
}

func TestClaudeCodePUTFamilyModelMapAndIgnoresAliases(t *testing.T) {
	configPath := claudeCodeConfigPath(t, `{"claudeCode":{"enabled":true}}`)
	h := claudeCodeContractHandlerAt(t, configPath, nil)
	body := `{
		"modelMap":{
			"opus":"gpt-5.6-sol",
			"sonnet":"gpt-5.6-sol",
			"haiku":"gpt-5.6-mini",
			"fable":"gpt-5.6-terra"
		},
		"aliases":[{"id":"opus","display_name":"Opus"}]
	}`
	if rr := claudeCodeDo(t, h, http.MethodPut, body); rr.Code != http.StatusOK {
		t.Fatalf("put status=%d body=%s", rr.Code, rr.Body.String())
	}
	got := claudeCodeGET(t, h)
	modelMap, _ := got["modelMap"].(map[string]any)
	tierModels, _ := got["tierModels"].(map[string]any)
	if modelMap["opus"] != "gpt-5.6-sol" || modelMap["sonnet"] != "gpt-5.6-sol" || modelMap["haiku"] != "gpt-5.6-mini" || modelMap["fable"] != "gpt-5.6-terra" {
		t.Fatalf("family map=%v", modelMap)
	}
	if fmtMap(tierModels) != fmtMap(modelMap) {
		t.Fatalf("GET modelMap and tierModels must project the same families: map=%v tier=%v", modelMap, tierModels)
	}
	aliases, _ := got["aliases"].([]any)
	if len(aliases) != 0 {
		t.Fatalf("PUT must not persist editable aliases, got %v", aliases)
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"aliases"`) {
		t.Fatalf("aliases leaked into disk: %s", raw)
	}
	var disk map[string]any
	if err := json.Unmarshal(raw, &disk); err != nil {
		t.Fatal(err)
	}
	block, _ := disk["claudeCode"].(map[string]any)
	stored, _ := block["tierModels"].(map[string]any)
	if stored["opus"] != "gpt-5.6-sol" || stored["fable"] != "gpt-5.6-terra" {
		t.Fatalf("canonical disk tierModels=%v", stored)
	}
	if _, ok := block["modelMap"]; ok {
		t.Fatalf("family PUT must not keep a second writable modelMap: %s", raw)
	}
}

func TestClaudeCodePUTRefusesInvalidSupportedFieldsWithExactEnvelope(t *testing.T) {
	h := claudeCodeContractHandler(t, `{"claudeCode":{"enabled":true}}`, nil)
	cases := []struct {
		body    string
		message string
	}{
		{`{"authMode":"oauth"}`, `authMode must be "auto", "proxy", or "subscription"`},
		{`{"autoCompactWindow":99999}`, `autoCompactWindow must be an integer between 100000 and 1000000, or null`},
		{`{"autoCompactWindow":1000000.5}`, `autoCompactWindow must be an integer between 100000 and 1000000, or null`},
		{`{"autoContext":"yes"}`, `autoContext must be a boolean`},
		{`{"injectAgents":1}`, `injectAgents must be a boolean`},
		{`{"smallFastModel":false}`, `smallFastModel must be a string`},
		{`{"modelMap":["opus"]}`, `modelMap must be an object`},
		{`{"modelMap":{"opus":1}}`, `modelMap.opus must be a string`},
		{`{"tierModels":{"sonnet":true}}`, `tierModels.sonnet must be a string`},
	}
	for _, tc := range cases {
		rr := claudeCodeDo(t, h, http.MethodPut, tc.body)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("status=%d body=%s for %s", rr.Code, rr.Body.String(), tc.body)
		}
		var envelope struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		if envelope.Error.Code != "invalid_body" || envelope.Error.Message != tc.message {
			t.Fatalf("envelope=%+v want %q for %s", envelope.Error, tc.message, tc.body)
		}
	}
}

func TestClaudeCodePUTRejectsRetiredFieldsByPresence(t *testing.T) {
	h := claudeCodeContractHandler(t, `{"claudeCode":{"enabled":true,"authMode":"proxy"}}`, nil)
	wantSuffix := " is no longer supported by the Claude Code settings contract"
	cases := []struct {
		field string
		body  string
	}{
		{"systemEnv", `{"systemEnv":true}`},
		{"systemEnv", `{"systemEnv":false}`},
		{"systemEnv", `{"systemEnv":null}`},
		{"fastMode", `{"fastMode":true}`},
		{"fastMode", `{"fastMode":false}`},
		{"fastMode", `{"fastMode":null}`},
		{"webSearchSidecar", `{"webSearchSidecar":{"backend":"openai"}}`},
		{"webSearchSidecar", `{"webSearchSidecar":null}`},
		{"visionSidecar", `{"visionSidecar":{"backend":"openai"}}`},
		{"visionSidecar", `{"visionSidecar":null}`},
		{"systemEnv", `{"authMode":"subscription","systemEnv":false}`},
	}
	for _, tc := range cases {
		rr := claudeCodeDo(t, h, http.MethodPut, tc.body)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("status=%d body=%s for %s", rr.Code, rr.Body.String(), tc.body)
		}
		var envelope struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		if envelope.Error.Code != "invalid_body" {
			t.Fatalf("code=%q for %s", envelope.Error.Code, tc.body)
		}
		if envelope.Error.Message != tc.field+wantSuffix {
			t.Fatalf("message=%q want %q for %s", envelope.Error.Message, tc.field+wantSuffix, tc.body)
		}
	}
	got := claudeCodeGET(t, h)
	if got["authMode"] != "proxy" {
		t.Fatalf("rejected retired PUT must not apply mixed supported fields, authMode=%v", got["authMode"])
	}
}

func TestClaudeCodeSupportedPUTPreservesHistoricalRetiredValues(t *testing.T) {
	configPath := claudeCodeConfigPath(t, `{
		"claudeCode":{
			"enabled":true,
			"systemEnv":true,
			"webSearchSidecar":{"backend":"openai","model":"kept-search"},
			"visionSidecar":{"backend":"openai","model":"kept-vision"}
		},
		"fastMode":true
	}`)
	h := claudeCodeContractHandlerAt(t, configPath, nil)
	got := claudeCodeGET(t, h)
	for _, key := range []string{"systemEnv", "fastMode", "webSearchSidecar", "visionSidecar"} {
		if _, ok := got[key]; ok {
			t.Fatalf("GET must omit retired field %s: %v", key, got[key])
		}
	}
	body := `{
		"blockedSkills":["web-search"],
		"modelMap":{"claude-3-opus-20240229":"intercepted","opus":"gpt-5.6-sol"}
	}`
	if rr := claudeCodeDo(t, h, http.MethodPut, body); rr.Code != http.StatusOK {
		t.Fatalf("put status=%d body=%s", rr.Code, rr.Body.String())
	}
	got = claudeCodeGET(t, h)
	for _, key := range []string{"systemEnv", "fastMode", "webSearchSidecar", "visionSidecar"} {
		if _, ok := got[key]; ok {
			t.Fatalf("supported PUT must not expose retired field %s: %v", key, got[key])
		}
	}
	skills, _ := got["blockedSkills"].([]any)
	if len(skills) != 1 || skills[0] != "web-search" {
		t.Fatalf("blockedSkills=%v", got["blockedSkills"])
	}
	modelMap, _ := got["modelMap"].(map[string]any)
	if modelMap["opus"] != "gpt-5.6-sol" {
		t.Fatalf("family route lost: %v", modelMap)
	}
	if _, ok := modelMap["claude-3-opus-20240229"]; ok {
		t.Fatalf("arbitrary interception must not be projected as routing: %v", modelMap)
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	var disk map[string]any
	if err := json.Unmarshal(raw, &disk); err != nil {
		t.Fatal(err)
	}
	if disk["fastMode"] != true {
		t.Fatalf("unrelated PUT must not rewrite root fastMode, got %v", disk["fastMode"])
	}
	block, _ := disk["claudeCode"].(map[string]any)
	if block["systemEnv"] != true {
		t.Fatalf("stored systemEnv must survive supported PUT, got %v", block["systemEnv"])
	}
	search, _ := block["webSearchSidecar"].(map[string]any)
	if search["model"] != "kept-search" {
		t.Fatalf("stored claudeCode sidecar must survive supported PUT, got %v", search)
	}
	vision, _ := block["visionSidecar"].(map[string]any)
	if vision["model"] != "kept-vision" {
		t.Fatalf("stored vision sidecar must survive supported PUT, got %v", vision)
	}
	if _, ok := block["fastMode"]; ok {
		t.Fatal("fastMode must not be nested under claudeCode")
	}
}

func TestClaudeCodeFamilyPUTSurvivesGETAndDisk(t *testing.T) {
	configPath := claudeCodeConfigPath(t, `{"claudeCode":{"enabled":true,"modelMap":{"opus":"legacy-opus","claude-3":"intercepted"}}}`)
	h := claudeCodeContractHandlerAt(t, configPath, nil)
	before := claudeCodeGET(t, h)
	beforeMap, _ := before["modelMap"].(map[string]any)
	if beforeMap["opus"] != "legacy-opus" {
		t.Fatalf("legacy family must not be lost before save: %v", beforeMap)
	}
	if _, ok := beforeMap["claude-3"]; ok {
		t.Fatal("legacy interception must not be effective routing")
	}
	body := `{"modelMap":{"opus":"gpt-5.6-sol","sonnet":"gpt-5.6-sol","haiku":"gpt-5.6-mini","fable":"gpt-5.6-terra"}}`
	if rr := claudeCodeDo(t, h, http.MethodPut, body); rr.Code != http.StatusOK {
		t.Fatalf("put status=%d body=%s", rr.Code, rr.Body.String())
	}
	got := claudeCodeGET(t, h)
	want := map[string]any{
		"opus":   "gpt-5.6-sol",
		"sonnet": "gpt-5.6-sol",
		"haiku":  "gpt-5.6-mini",
		"fable":  "gpt-5.6-terra",
	}
	if fmtMap(got["modelMap"]) != fmtMap(want) || fmtMap(got["tierModels"]) != fmtMap(want) {
		t.Fatalf("get map=%v tier=%v", got["modelMap"], got["tierModels"])
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	var disk map[string]any
	if err := json.Unmarshal(raw, &disk); err != nil {
		t.Fatal(err)
	}
	block, _ := disk["claudeCode"].(map[string]any)
	if fmtMap(block["tierModels"]) != fmtMap(want) {
		t.Fatalf("disk canonical=%v", block["tierModels"])
	}
	if _, ok := block["modelMap"]; ok {
		t.Fatalf("disk still has modelMap: %s", raw)
	}
}

func TestClaudeCodeEmptyFamilyPUTRestoresUnsetDefaults(t *testing.T) {
	configPath := claudeCodeConfigPath(t, `{"claudeCode":{"tierModels":{"opus":"gpt-5.6-sol","sonnet":"gpt-5.6-sol"}}}`)
	h := claudeCodeContractHandlerAt(t, configPath, nil)
	if rr := claudeCodeDo(t, h, http.MethodPut, `{"modelMap":{}}`); rr.Code != http.StatusOK {
		t.Fatalf("put status=%d body=%s", rr.Code, rr.Body.String())
	}
	got := claudeCodeGET(t, h)
	if len(got["modelMap"].(map[string]any)) != 0 || len(got["tierModels"].(map[string]any)) != 0 {
		t.Fatalf("unset families=%v", got)
	}
	raw, _ := os.ReadFile(configPath)
	if strings.Contains(string(raw), `"tierModels"`) || strings.Contains(string(raw), `"modelMap"`) {
		t.Fatalf("empty families leaked onto disk: %s", raw)
	}
}

func TestClaudeCodeCompactWindowStoredVersusEffective(t *testing.T) {
	configPath := claudeCodeConfigPath(t, `{"claudeCode":{"enabled":true}}`)
	h := claudeCodeContractHandlerAt(t, configPath, nil)
	got := claudeCodeGET(t, h)
	if got["autoCompactWindow"] != nil {
		t.Fatalf("absent compact must stay null, got %v", got["autoCompactWindow"])
	}
	if rr := claudeCodeDo(t, h, http.MethodPut, `{"authMode":"proxy"}`); rr.Code != http.StatusOK {
		t.Fatalf("unrelated put status=%d body=%s", rr.Code, rr.Body.String())
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "829800") || strings.Contains(string(raw), "autoCompactWindow") {
		t.Fatalf("unrelated save materialized compact default: %s", raw)
	}
	if rr := claudeCodeDo(t, h, http.MethodPut, `{"autoCompactWindow":350000}`); rr.Code != http.StatusOK {
		t.Fatalf("put compact status=%d body=%s", rr.Code, rr.Body.String())
	}
	got = claudeCodeGET(t, h)
	if compact, _ := got["autoCompactWindow"].(float64); compact != 350000 {
		t.Fatalf("stored compact=%v", got["autoCompactWindow"])
	}
	if rr := claudeCodeDo(t, h, http.MethodPut, `{"autoCompactWindow":null}`); rr.Code != http.StatusOK {
		t.Fatalf("reset compact status=%d body=%s", rr.Code, rr.Body.String())
	}
	got = claudeCodeGET(t, h)
	if got["autoCompactWindow"] != nil {
		t.Fatalf("PUT null must delete stored compact, got %v", got["autoCompactWindow"])
	}
	raw, _ = os.ReadFile(configPath)
	if strings.Contains(string(raw), "autoCompactWindow") {
		t.Fatalf("reset leaked compact onto disk: %s", raw)
	}
}

func fmtMap(value any) string {
	raw, _ := json.Marshal(value)
	return string(raw)
}

func claudeCodeContractHandler(t *testing.T, configJSON string, models []catalog.Model) http.Handler {
	t.Helper()
	return claudeCodeContractHandlerAt(t, claudeCodeConfigPath(t, configJSON), models)
}

func claudeCodeContractHandlerAt(t *testing.T, configPath string, models []catalog.Model) http.Handler {
	t.Helper()
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
		CatalogModels:  models,
		ConfigPath:     configPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	return attachHandlerClose(t, h)
}

func claudeCodeConfigPath(t *testing.T, configJSON string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(configJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func claudeCodeDo(t *testing.T, h http.Handler, method, body string) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, "/api/claude-code", nil)
	} else {
		req = httptest.NewRequest(method, "/api/claude-code", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func claudeCodeGET(t *testing.T, h http.Handler) map[string]any {
	t.Helper()
	rr := claudeCodeDo(t, h, http.MethodGet, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", rr.Code, rr.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	return got
}
