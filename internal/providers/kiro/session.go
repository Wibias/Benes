package kiro

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const (
	defaultExpiresMS = int64(3600_000)
	cliUnixName      = "kiro-cli"
	cliWindowsName   = "kiro-cli.exe"
)

var (
	ErrNoSession       = fmt.Errorf("Kiro: no local CLI or credential-file session")
	ErrTokenAmbiguous  = fmt.Errorf("Kiro CLI credential database contains multiple tokens; set KIROCLI_TOKEN_KEY to select one")
	ErrTokenKeyMissing = fmt.Errorf("The KIROCLI_TOKEN_KEY selection was not found in the Kiro CLI credential database")
)

var preferredTokenKeys = []string{
	"kirocli:odic:token",
	"kirocli:oidc:token",
	"kirocli:social:token",
	"codewhisperer:odic:token",
}

var registrationKeys = []string{
	"kirocli:odic:device-registration",
	"kirocli:oidc:device-registration",
	"codewhisperer:odic:device-registration",
}

type Host struct {
	Platform  string
	Home      string
	Env       map[string]string
	Now       func() time.Time
	AfterRead func()
}

type ImportDiagnostic struct {
	Location string
	Status   string
}

type SessionImport struct {
	Credential  ImportedCredential
	Source      string
	Diagnostics []ImportDiagnostic
}

type sessionEntry struct {
	location string
	path     string
}

func LiveHost() Host {
	env := map[string]string{}
	for _, kv := range os.Environ() {
		key, value, ok := strings.Cut(kv, "=")
		if ok {
			env[key] = value
		}
	}
	home, _ := os.UserHomeDir()
	if runtime.GOOS != "windows" {
		if fromEnv := strings.TrimSpace(os.Getenv("HOME")); fromEnv != "" {
			home = fromEnv
		}
	}
	return Host{Platform: runtime.GOOS, Home: home, Env: env}
}

func (h Host) getenv(keys ...string) string {
	for _, key := range keys {
		if h.Env == nil {
			continue
		}
		if value := strings.TrimSpace(h.Env[key]); value != "" {
			return value
		}
	}
	return ""
}

func (h Host) nowMS() int64 {
	if h.Now != nil {
		return h.Now().UnixMilli()
	}
	return time.Now().UnixMilli()
}

func (h Host) join(elem ...string) string {
	var parts []string
	for i, raw := range elem {
		part := strings.ReplaceAll(raw, `\`, "/")
		if i > 0 {
			part = strings.TrimPrefix(part, "/")
		}
		part = strings.TrimSuffix(part, "/")
		if part != "" || i == 0 {
			parts = append(parts, part)
		}
	}
	out := strings.Join(parts, "/")
	if h.Platform == "windows" {
		return strings.ReplaceAll(out, "/", `\`)
	}
	return out
}

func (h Host) absolute(path string) bool {
	if h.Platform == "windows" {
		if strings.HasPrefix(path, `\\`) {
			return true
		}
		return len(path) >= 3 && path[1] == ':' && (path[2] == '\\' || path[2] == '/')
	}
	return strings.HasPrefix(path, "/")
}

func (h Host) expand(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		return h.join(h.Home, path[2:])
	}
	if filepath.IsAbs(path) || h.absolute(path) {
		return path
	}
	cwd, _ := os.Getwd()
	return h.join(cwd, path)
}

func NativeSessionEntry(h Host) (string, string) {
	switch h.Platform {
	case "windows":
		base := h.getenv("LOCALAPPDATA")
		if base == "" {
			if profile := h.getenv("USERPROFILE"); profile != "" {
				base = h.join(profile, "AppData", "Local")
			} else {
				base = h.join(h.Home, "AppData", "Local")
			}
		}
		return "kiro-cli-windows-data", h.join(base, "Kiro-Cli", "data.sqlite3")
	case "darwin":
		return "kiro-cli-data", h.join(h.Home, "Library", "Application Support", "kiro-cli", "data.sqlite3")
	default:
		return "kiro-cli-linux-data", h.join(h.Home, ".local", "share", "kiro-cli", "data.sqlite3")
	}
}

func sqliteEntries(h Host) []sessionEntry {
	if configured := h.getenv("KIROCLI_DB_PATH", "KIRO_CLI_DB_FILE"); configured != "" {
		return []sessionEntry{{location: "kiro-cli-db-env", path: h.expand(configured)}}
	}
	location, path := NativeSessionEntry(h)
	return []sessionEntry{
		{location: location, path: path},
		{location: "amazon-q-data", path: h.join(h.Home, ".local", "share", "amazon-q", "data.sqlite3")},
		{location: "kiro-sso-cache", path: h.join(h.Home, ".kiro", "sso", "cache.db")},
	}
}

func jsonCredentialPaths(h Host) []string {
	var out []string
	for _, key := range []string{"KIRO_CREDS_FILE", "KIRO_CREDENTIALS_FILE"} {
		if path := h.getenv(key); path != "" {
			out = append(out, h.expand(path))
		}
	}
	return out
}

func isRegularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func ResolveCLIExecutable(h Host) string {
	listSep := ":"
	if h.Platform == "windows" {
		listSep = ";"
	}
	var candidates []string
	for _, entry := range strings.Split(h.getenv("PATH", "Path"), listSep) {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if h.Platform == "windows" {
			candidates = append(candidates, h.join(entry, cliWindowsName), h.join(entry, cliUnixName))
		} else {
			candidates = append(candidates, h.join(entry, cliUnixName))
		}
	}
	if h.Platform == "windows" {
		local := h.getenv("LOCALAPPDATA")
		if local == "" {
			if profile := h.getenv("USERPROFILE"); profile != "" {
				local = h.join(profile, "AppData", "Local")
			} else {
				local = h.join(h.Home, "AppData", "Local")
			}
		}
		programFiles := h.getenv("ProgramFiles")
		if programFiles == "" {
			programFiles = `C:\Program Files`
		}
		candidates = append(candidates,
			h.join(local, "Kiro-Cli", cliWindowsName),
			h.join(programFiles, "Kiro-Cli", cliWindowsName),
		)
	} else if h.Platform == "darwin" {
		candidates = append(candidates,
			h.join(h.Home, ".local", "bin", cliUnixName),
			"/usr/local/bin/"+cliUnixName,
			"/opt/homebrew/bin/"+cliUnixName,
		)
	} else {
		candidates = append(candidates,
			h.join(h.Home, ".local", "bin", cliUnixName),
			"/usr/local/bin/"+cliUnixName,
		)
	}
	for _, candidate := range candidates {
		if isRegularFile(candidate) {
			return candidate
		}
	}
	if h.Platform == "windows" {
		return cliWindowsName
	}
	return cliUnixName
}

func ImportLocalSession(h Host) (SessionImport, error) {
	var diags []ImportDiagnostic
	if cred, ok, err := readJSONCredentials(h, &diags); err != nil {
		return SessionImport{Diagnostics: diags}, err
	} else if ok {
		return SessionImport{Credential: cred, Source: "json", Diagnostics: diags}, nil
	}
	if cred, ok, err := readSQLiteCredentials(h, &diags); err != nil {
		return SessionImport{Diagnostics: diags}, err
	} else if ok {
		return SessionImport{Credential: cred, Source: "sqlite", Diagnostics: diags}, nil
	}
	return SessionImport{Diagnostics: diags}, ErrNoSession
}

func ImportLocalSessionStable(h Host) (SessionImport, error) {
	first, err := ImportLocalSession(h)
	if err != nil {
		return first, err
	}
	if h.AfterRead != nil {
		h.AfterRead()
	}
	second, err := ImportLocalSession(h)
	if err != nil {
		return second, err
	}
	if err := RevalidateImported(first.Credential, second.Credential); err != nil {
		return SessionImport{Diagnostics: append(append([]ImportDiagnostic{}, first.Diagnostics...), second.Diagnostics...)}, err
	}
	return first, nil
}

func RevalidateImported(current, latest ImportedCredential) error {
	if current.AccessToken != latest.AccessToken || current.ProfileARN != latest.ProfileARN || importedRegion(current) != importedRegion(latest) {
		return ErrSplitSnapshot
	}
	return nil
}

func importedRegion(cred ImportedCredential) string {
	if region := NormalizeRegion(cred.APIRegion); region != "" {
		return region
	}
	if region := InferRegionFromProfileARN(cred.ProfileARN); region != "" {
		return region
	}
	return NormalizeRegion(cred.SSORegion)
}

func readJSONCredentials(h Host, diags *[]ImportDiagnostic) (ImportedCredential, bool, error) {
	for _, path := range jsonCredentialPaths(h) {
		if _, err := os.Stat(path); err != nil {
			*diags = append(*diags, ImportDiagnostic{Location: "kiro-creds-file", Status: "missing"})
			continue
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			*diags = append(*diags, ImportDiagnostic{Location: "kiro-creds-file", Status: "unreadable"})
			continue
		}
		data, err := decodeObject(raw)
		if err != nil {
			*diags = append(*diags, ImportDiagnostic{Location: "kiro-creds-file", Status: "invalid_json"})
			continue
		}
		if registration := loadEnterpriseRegistration(h, data); registration != nil {
			data = mergeObjects(registration, data)
		}
		cred, ok := credentialFromJSON(data, h.nowMS())
		if !ok {
			*diags = append(*diags, ImportDiagnostic{Location: "kiro-creds-file", Status: "token_missing"})
			continue
		}
		final, err := finalizeImported(cred)
		if err != nil {
			return ImportedCredential{}, false, err
		}
		*diags = append(*diags, ImportDiagnostic{Location: "kiro-creds-file", Status: "token_found"})
		return final, true, nil
	}
	return ImportedCredential{}, false, nil
}

func readSQLiteCredentials(h Host, diags *[]ImportDiagnostic) (ImportedCredential, bool, error) {
	for _, entry := range sqliteEntries(h) {
		if _, err := os.Stat(entry.path); err != nil {
			*diags = append(*diags, ImportDiagnostic{Location: entry.location, Status: "missing"})
			continue
		}
		db, err := openSQLite(entry.path)
		if err != nil {
			*diags = append(*diags, ImportDiagnostic{Location: entry.location, Status: "unreadable"})
			continue
		}
		cred, status, err := readSQLiteToken(db, h)
		_ = db.Close()
		if err != nil {
			*diags = append(*diags, ImportDiagnostic{Location: entry.location, Status: status})
			return ImportedCredential{}, false, err
		}
		*diags = append(*diags, ImportDiagnostic{Location: entry.location, Status: status})
		if status == "token_found" {
			return cred, true, nil
		}
	}
	return ImportedCredential{}, false, nil
}

func readSQLiteToken(db *sql.DB, h Host) (ImportedCredential, string, error) {
	rows, err := db.Query(`SELECT key, value FROM auth_kv WHERE key LIKE ? ORDER BY key ASC`, "%:token")
	if err != nil {
		return ImportedCredential{}, "schema_mismatch", nil
	}
	defer rows.Close()
	type kv struct{ key, value string }
	var tokens []kv
	for rows.Next() {
		var row kv
		if err := rows.Scan(&row.key, &row.value); err != nil {
			return ImportedCredential{}, "unreadable", nil
		}
		tokens = append(tokens, row)
	}
	if err := rows.Err(); err != nil {
		return ImportedCredential{}, "unreadable", nil
	}
	selected := h.getenv("KIROCLI_TOKEN_KEY")
	var chosen *kv
	switch {
	case selected != "":
		for i := range tokens {
			if tokens[i].key == selected {
				chosen = &tokens[i]
				break
			}
		}
		if chosen == nil {
			return ImportedCredential{}, "token_key_missing", ErrTokenKeyMissing
		}
	default:
		for _, preferred := range preferredTokenKeys {
			for i := range tokens {
				if tokens[i].key == preferred {
					chosen = &tokens[i]
					break
				}
			}
			if chosen != nil {
				break
			}
		}
		if chosen == nil {
			if len(tokens) == 0 {
				return ImportedCredential{}, "token_missing", nil
			}
			if len(tokens) > 1 {
				return ImportedCredential{}, "token_ambiguous", ErrTokenAmbiguous
			}
			chosen = &tokens[0]
		}
	}
	tokenData, err := decodeObject([]byte(chosen.value))
	if err != nil {
		return ImportedCredential{}, "invalid_json", nil
	}
	registration := map[string]any{}
	for _, key := range registrationKeys {
		var value string
		if err := db.QueryRow(`SELECT value FROM auth_kv WHERE key = ?`, key).Scan(&value); err != nil {
			continue
		}
		parsed, err := decodeObject([]byte(value))
		if err != nil {
			continue
		}
		registration = parsed
		break
	}
	profile := readStateProfile(db)
	merged := mergeObjects(registration, tokenData, profile)
	cred, ok := credentialFromJSON(merged, h.nowMS())
	if !ok {
		return ImportedCredential{}, "token_missing", nil
	}
	final, err := finalizeImported(cred)
	if err != nil {
		return ImportedCredential{}, "token_found", err
	}
	return final, "token_found", nil
}

func readStateProfile(db *sql.DB) map[string]any {
	var value string
	if err := db.QueryRow(`SELECT value FROM state WHERE key = ?`, "api.codewhisperer.profile").Scan(&value); err != nil {
		return nil
	}
	data, err := decodeObject([]byte(value))
	if err != nil {
		return nil
	}
	arn := stringField(data, "arn", "profileArn", "profile_arn")
	if arn == "" {
		return nil
	}
	return map[string]any{"profileArn": arn, "apiRegion": InferRegionFromProfileARN(arn)}
}

func loadEnterpriseRegistration(h Host, data map[string]any) map[string]any {
	hash := stringField(data, "clientIdHash")
	if hash == "" || !clientIDHashOK(hash) {
		return nil
	}
	path := h.join(h.Home, ".aws", "sso", "cache", hash+".json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	parsed, err := decodeObject(raw)
	if err != nil {
		return nil
	}
	return parsed
}

func clientIDHashOK(hash string) bool {
	if len(hash) == 0 || len(hash) > 128 {
		return false
	}
	for _, r := range hash {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func credentialFromJSON(data map[string]any, nowMS int64) (ImportedCredential, bool) {
	access := stringField(data, "accessToken", "access_token")
	if access == "" {
		return ImportedCredential{}, false
	}
	profile := stringField(data, "profileArn", "profile_arn")
	sso := stringField(data, "region")
	api := stringField(data, "apiRegion", "api_region")
	if api == "" {
		api = InferRegionFromProfileARN(profile)
	}
	if api == "" {
		api = sso
	}
	refresh := stringField(data, "refreshToken", "refresh_token")
	_, expiresPresent := lookup(data, "expiresAt", "expires_at")
	return ImportedCredential{
		AccessToken:  access,
		Refresh:      refresh,
		ProfileARN:   profile,
		APIRegion:    api,
		SSORegion:    sso,
		ClientID:     stringField(data, "clientId", "client_id"),
		ClientSecret: stringField(data, "clientSecret", "client_secret"),
		ExpiresUnix:  parseExpires(data["expiresAt"], data["expires_at"], expiresPresent, refresh != "", nowMS),
	}, true
}

func finalizeImported(cred ImportedCredential) (ImportedCredential, error) {
	if cred.ClientID != "" && cred.ClientSecret != "" {
		cred.AuthType = AuthAWSSsoOIDC
	} else {
		cred.AuthType = AuthKiroDesktop
	}
	if inferred := InferRegionFromProfileARN(cred.ProfileARN); inferred != "" {
		if cred.APIRegion == "" {
			cred.APIRegion = inferred
		} else if NormalizeRegion(cred.APIRegion) != inferred {
			return ImportedCredential{}, fmt.Errorf("%w: imported apiRegion does not match profileArn", ErrSplitSnapshot)
		}
	}
	return cred, nil
}

func parseExpires(expiresAt, expiresAtSnake any, present, hasRefresh bool, nowMS int64) int64 {
	value := expiresAt
	if value == nil {
		value = expiresAtSnake
	}
	switch typed := value.(type) {
	case float64:
		if typed < 10_000_000_000 {
			return int64(typed * 1000)
		}
		return int64(typed)
	case json.Number:
		n, err := typed.Float64()
		if err == nil {
			if n < 10_000_000_000 {
				return int64(n * 1000)
			}
			return int64(n)
		}
	case string:
		if typed != "" {
			if parsed, err := time.Parse(time.RFC3339, typed); err == nil {
				return parsed.UnixMilli()
			}
			if parsed, err := time.Parse(time.RFC3339Nano, typed); err == nil {
				return parsed.UnixMilli()
			}
		}
	}
	if present && hasRefresh {
		return 0
	}
	return nowMS + defaultExpiresMS
}

func stringField(data map[string]any, keys ...string) string {
	value, _ := lookup(data, keys...)
	text, _ := value.(string)
	return strings.TrimSpace(text)
}

func lookup(data map[string]any, keys ...string) (any, bool) {
	for _, key := range keys {
		if value, ok := data[key]; ok {
			return value, true
		}
	}
	return nil, false
}

func decodeObject(raw []byte) (map[string]any, error) {
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, err
	}
	return data, nil
}

func openSQLite(path string) (*sql.DB, error) {
	return openSQLiteMode(path, true)
}

func openSQLiteWritable(path string) (*sql.DB, error) {
	return openSQLiteMode(path, false)
}

func openSQLiteMode(path string, queryOnly bool) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	_, _ = db.Exec("PRAGMA busy_timeout = 5000")
	if queryOnly {
		_, _ = db.Exec("PRAGMA query_only = ON")
	}
	return db, nil
}

func mergeObjects(parts ...map[string]any) map[string]any {
	out := map[string]any{}
	for _, part := range parts {
		for key, value := range part {
			out[key] = value
		}
	}
	return out
}
