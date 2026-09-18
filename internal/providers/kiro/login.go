package kiro

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

const (
	recoverySuffix     = ".benes-recovery"
	recoveryHeaderV2   = "benes-kiro-session-v2\n"
	sqliteHeader       = "SQLite format 3\x00"
	unixInstallHint    = "install the Kiro CLI (`curl -fsSL https://cli.kiro.dev/install | bash`)"
	windowsInstallHint = "install the Kiro CLI in PowerShell (`irm 'https://cli.kiro.dev/install.ps1' | iex`)"
)

var processInstance = newProcessInstance()

type CLIResult struct {
	ExitCode int
	Stdout   string
}

type CLIRunner func(ctx context.Context, args []string) (CLIResult, error)

type SessionSnapshot struct {
	Path         string
	Database     []byte
	RecoveryPath string
}

type PendingLogin struct {
	Import     SessionImport
	Snapshot   *SessionSnapshot
	EmptyPrior bool
}

func InstallGuidance(platform string) string {
	if platform == "windows" {
		return windowsInstallHint
	}
	return unixInstallHint
}

func Login(ctx context.Context, h Host, runner CLIRunner) (PendingLogin, error) {
	if err := RestoreStaleRecovery(h); err != nil {
		return PendingLogin{}, err
	}
	if err := ctxErr(ctx); err != nil {
		return PendingLogin{}, err
	}
	imported, err := ImportLocalSessionStable(h)
	if err != nil {
		return PendingLogin{}, err
	}
	imported = fillWhoami(ctx, h, runner, imported)
	if _, err := ImportSnapshot(imported.Credential); err != nil {
		return PendingLogin{}, err
	}
	return PendingLogin{Import: imported}, nil
}

func ForceLogin(ctx context.Context, h Host, runner CLIRunner) (PendingLogin, error) {
	if runner == nil {
		return PendingLogin{}, fmt.Errorf("Kiro CLI is not installed or could not be started.")
	}
	if err := RestoreStaleRecovery(h); err != nil {
		return PendingLogin{}, err
	}
	if err := ctxErr(ctx); err != nil {
		return PendingLogin{}, err
	}
	snap, _, blocked := InspectNativeSession(h)
	if blocked {
		return PendingLogin{}, fmt.Errorf("Kiro CLI session could not be backed up, so a forced login will not sign it out")
	}
	if snap != nil {
		if err := PersistRecovery(*snap); err != nil {
			return PendingLogin{}, err
		}
	}
	pending := PendingLogin{Snapshot: snap, EmptyPrior: snap == nil}
	imported, err := runForcedLogin(ctx, h, runner)
	if err != nil {
		restoreForced(pending)
		return PendingLogin{}, err
	}
	pending.Import = imported
	return pending, nil
}

func Settle(pending PendingLogin, persisted bool) {
	if persisted {
		if pending.Snapshot != nil {
			DiscardRecovery(*pending.Snapshot)
		}
		return
	}
	restoreForced(pending)
}

func restoreForced(pending PendingLogin) {
	if pending.Snapshot != nil {
		_ = RestoreSession(*pending.Snapshot)
		DiscardRecovery(*pending.Snapshot)
		return
	}
	if pending.EmptyPrior {
		_ = exec.Command(ResolveCLIExecutable(LiveHost()), "logout").Run()
	}
}

func runForcedLogin(ctx context.Context, h Host, runner CLIRunner) (SessionImport, error) {
	if err := ctxErr(ctx); err != nil {
		return SessionImport{}, err
	}
	logout, err := runner(ctx, []string{"logout"})
	if err != nil || logout.ExitCode != 0 {
		if err != nil {
			return SessionImport{}, err
		}
		return SessionImport{}, fmt.Errorf("Kiro CLI could not prepare a fresh login.")
	}
	if err := ctxErr(ctx); err != nil {
		return SessionImport{}, err
	}
	login, err := runner(ctx, []string{"login"})
	if err != nil || login.ExitCode != 0 {
		if err != nil {
			return SessionImport{}, err
		}
		return SessionImport{}, fmt.Errorf("Kiro CLI login did not complete successfully.")
	}
	imported, err := ImportLocalSession(h)
	if err != nil {
		return SessionImport{}, fmt.Errorf("Kiro CLI login completed but no credential could be imported")
	}
	imported = fillWhoami(ctx, h, runner, imported)
	if _, err := ImportSnapshot(imported.Credential); err != nil {
		return SessionImport{}, err
	}
	return imported, nil
}

func fillWhoami(ctx context.Context, h Host, runner CLIRunner, imported SessionImport) SessionImport {
	if runner == nil || imported.Source != "sqlite" || imported.Credential.ProfileARN != "" {
		return imported
	}
	result, err := runner(ctx, []string{"whoami", "--format", "json"})
	if err != nil || result.ExitCode != 0 {
		return imported
	}
	var parsed map[string]any
	if json.Unmarshal([]byte(result.Stdout), &parsed) != nil {
		return imported
	}
	arn := stringField(parsed, "profileArn", "profile_arn")
	if arn == "" {
		if profile, _ := parsed["profile"].(map[string]any); profile != nil {
			arn = stringField(profile, "arn")
		}
	}
	if arn == "" || !profileARNPattern.MatchString(arn) {
		return imported
	}
	current, err := ImportLocalSession(h)
	if err != nil {
		return imported
	}
	importedKey := imported.Credential.Refresh
	if importedKey == "" {
		importedKey = imported.Credential.AccessToken
	}
	currentKey := current.Credential.Refresh
	if currentKey == "" {
		currentKey = current.Credential.AccessToken
	}
	if importedKey == "" || importedKey != currentKey {
		return imported
	}
	imported.Credential.ProfileARN = arn
	if imported.Credential.APIRegion == "" {
		imported.Credential.APIRegion = InferRegionFromProfileARN(arn)
	}
	return imported
}

func InspectNativeSession(h Host) (*SessionSnapshot, []ImportDiagnostic, bool) {
	var diags []ImportDiagnostic
	if h.getenv("KIROCLI_DB_PATH", "KIRO_CLI_DB_FILE") != "" {
		diags = append(diags, ImportDiagnostic{Location: "kiro-cli-db-env", Status: "token_ambiguous"})
		return nil, diags, true
	}
	location, path := NativeSessionEntry(h)
	if _, err := os.Stat(path); err != nil {
		diags = append(diags, ImportDiagnostic{Location: location, Status: "missing"})
		return nil, diags, false
	}
	imported, status, err := inspectSQLiteFile(path, h)
	diags = append(diags, ImportDiagnostic{Location: location, Status: status})
	if err != nil || status != "token_found" || imported.AccessToken == "" {
		return nil, diags, true
	}
	raw, err := snapshotDatabase(path)
	if err != nil {
		diags[len(diags)-1].Status = "unreadable"
		return nil, diags, true
	}
	return &SessionSnapshot{Path: path, Database: raw, RecoveryPath: path + recoverySuffix}, diags, false
}

func inspectSQLiteFile(path string, h Host) (ImportedCredential, string, error) {
	db, err := openSQLite(path)
	if err != nil {
		return ImportedCredential{}, "unreadable", err
	}
	defer db.Close()
	return readSQLiteToken(db, h)
}

func snapshotDatabase(path string) ([]byte, error) {
	nonce := newNonce()
	tmp := path + ".benes-snap." + nonce + ".tmp"
	defer os.Remove(tmp)
	db, err := openSQLiteWritable(path)
	if err != nil {
		return nil, err
	}
	_, vacErr := db.Exec("VACUUM INTO ?", tmp)
	_ = db.Close()
	if vacErr == nil {
		return os.ReadFile(tmp)
	}
	if _, err := os.Stat(path + "-wal"); err == nil {
		return nil, vacErr
	}
	return os.ReadFile(path)
}

func PersistRecovery(snap SessionSnapshot) error {
	if _, err := os.Stat(snap.RecoveryPath); err == nil {
		return fmt.Errorf("Kiro CLI session recovery is already pending.")
	}
	staged := snap.RecoveryPath + "." + newNonce() + ".tmp"
	payload := append([]byte(recoveryHeaderV2), []byte(strconv.Itoa(os.Getpid())+"\n")...)
	payload = append(payload, []byte(processInstance+"\n")...)
	payload = append(payload, snap.Database...)
	if err := os.WriteFile(staged, payload, 0o600); err != nil {
		return err
	}
	defer os.Remove(staged)
	if err := os.Link(staged, snap.RecoveryPath); err != nil {
		if err := os.Rename(staged, snap.RecoveryPath); err != nil {
			return err
		}
	}
	return nil
}

func DiscardRecovery(snap SessionSnapshot) {
	_ = os.Remove(snap.RecoveryPath)
}

func RestoreSession(snap SessionSnapshot) error {
	if len(snap.Database) < len(sqliteHeader) || string(snap.Database[:len(sqliteHeader)]) != sqliteHeader {
		return fmt.Errorf("Kiro CLI session recovery data is invalid")
	}
	nonce := newNonce()
	staged := snap.Path + ".benes-restore." + nonce + ".tmp"
	if err := os.WriteFile(staged, snap.Database, 0o600); err != nil {
		return err
	}
	defer os.Remove(staged)
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		_ = os.Remove(snap.Path + suffix)
	}
	return os.Rename(staged, snap.Path)
}

func RestoreStaleRecovery(h Host) error {
	_, path := NativeSessionEntry(h)
	recoveryPath := path + recoverySuffix
	raw, err := os.ReadFile(recoveryPath)
	if err != nil {
		return nil
	}
	pid, instance, database, ok := parseRecovery(raw)
	if !ok {
		return fmt.Errorf("Kiro CLI session recovery data is invalid")
	}
	if processAlive(pid, instance) {
		return fmt.Errorf("Another Kiro CLI login transaction is still in progress")
	}
	claimed := recoveryPath + ".claimed." + newNonce()
	if err := os.Rename(recoveryPath, claimed); err != nil {
		return nil
	}
	if err := RestoreSession(SessionSnapshot{Path: path, Database: database, RecoveryPath: claimed}); err != nil {
		_ = os.Rename(claimed, recoveryPath)
		return err
	}
	_ = os.Remove(claimed)
	return nil
}

func parseRecovery(payload []byte) (int, string, []byte, bool) {
	header := []byte(recoveryHeaderV2)
	if !strings.HasPrefix(string(payload), recoveryHeaderV2) {
		return 0, "", nil, false
	}
	rest := payload[len(header):]
	pidEnd := indexByte(rest, '\n')
	if pidEnd <= 0 {
		return 0, "", nil, false
	}
	pid, err := strconv.Atoi(string(rest[:pidEnd]))
	if err != nil || pid <= 0 {
		return 0, "", nil, false
	}
	rest = rest[pidEnd+1:]
	instEnd := indexByte(rest, '\n')
	if instEnd <= 0 {
		return 0, "", nil, false
	}
	instance := string(rest[:instEnd])
	database := rest[instEnd+1:]
	if len(database) < len(sqliteHeader) || string(database[:len(sqliteHeader)]) != sqliteHeader {
		return 0, "", nil, false
	}
	return pid, instance, database, true
}

func processAlive(pid int, instance string) bool {
	if pid == os.Getpid() {
		return instance == processInstance
	}
	return pidExists(pid)
}

func DefaultCLIRunner(h Host) CLIRunner {
	return func(ctx context.Context, args []string) (CLIResult, error) {
		if err := ctxErr(ctx); err != nil {
			return CLIResult{}, err
		}
		cmd := exec.CommandContext(ctx, ResolveCLIExecutable(h), args...)
		out, err := cmd.Output()
		if err != nil {
			if exit, ok := err.(*exec.ExitError); ok {
				return CLIResult{ExitCode: exit.ExitCode(), Stdout: string(out)}, nil
			}
			return CLIResult{}, fmt.Errorf("Kiro CLI is not installed or could not be started.")
		}
		return CLIResult{ExitCode: 0, Stdout: string(out)}, nil
	}
}

func newProcessInstance() string {
	raw := make([]byte, 8)
	if _, err := rand.Read(raw); err != nil {
		return "local"
	}
	return hex.EncodeToString(raw)
}

func newNonce() string {
	raw := make([]byte, 8)
	if _, err := rand.Read(raw); err != nil {
		return strconv.Itoa(os.Getpid())
	}
	return hex.EncodeToString(raw)
}

func indexByte(b []byte, c byte) int {
	for i, v := range b {
		if v == c {
			return i
		}
	}
	return -1
}

func ctxErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}
