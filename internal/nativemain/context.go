package nativemain

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Wibias/Benes/internal/config"
)

type Context struct {
	CodexHome         string
	ConfigDir         string
	InstanceID        string
	HomeID            string
	RootDir           string
	StagingRoot       string
	AuthPath          string
	VaultPath         string
	JournalPath       string
	RecoveryBlockPath string
	StageRegistryPath string
	LockPath          string
}

func ResolveContext(codexHome, configDir string) (Context, error) {
	if strings.TrimSpace(codexHome) == "" {
		if env := strings.TrimSpace(os.Getenv("CODEX_HOME")); env != "" {
			codexHome = env
		} else {
			home, err := os.UserHomeDir()
			if err != nil {
				return Context{}, fail("CODEX_HOME_UNAVAILABLE", "The effective CODEX_HOME is not an accessible directory.", 409)
			}
			codexHome = filepath.Join(home, ".codex")
		}
	}
	abs, err := filepath.Abs(codexHome)
	if err != nil {
		return Context{}, fail("CODEX_HOME_UNAVAILABLE", "The effective CODEX_HOME is not an accessible directory.", 409)
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return Context{}, fail("CODEX_HOME_UNAVAILABLE", "The effective CODEX_HOME is not an accessible directory.", 409)
	}
	if strings.TrimSpace(configDir) == "" {
		paths, err := config.ResolvePaths(config.PathOptions{})
		if err != nil {
			return Context{}, fail("PROFILE_STORAGE_UNSAFE", "The Benes configuration root is not safely accessible.", 409)
		}
		configDir = paths.Home
	}
	configAbs, err := filepath.Abs(configDir)
	if err != nil {
		return Context{}, fail("PROFILE_STORAGE_UNSAFE", "The Benes configuration root is not safely accessible.", 409)
	}
	homeID := shaHex(append([]byte(domainHome), []byte(abs)...))
	instanceID := shaHex(append([]byte(domainInstance), []byte(configAbs)...))
	root := filepath.Join(abs, sharedMetadataDir)
	return Context{
		CodexHome:         abs,
		ConfigDir:         configAbs,
		InstanceID:        instanceID,
		HomeID:            homeID,
		RootDir:           root,
		StagingRoot:       filepath.Join(configAbs, instanceStagingDir, homeID),
		AuthPath:          filepath.Join(abs, "auth.json"),
		VaultPath:         filepath.Join(root, homeID+".vault.json"),
		JournalPath:       filepath.Join(root, homeID+".journal.json"),
		RecoveryBlockPath: filepath.Join(root, homeID+".recovery-block.json"),
		StageRegistryPath: filepath.Join(root, homeID+".stages.json"),
		LockPath:          filepath.Join(abs, ".benes-native-profile.lock.sqlite"),
	}, nil
}

func shaHex(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func samePath(a, b string) bool {
	aa, _ := filepath.Abs(a)
	bb, _ := filepath.Abs(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(aa, bb)
	}
	return aa == bb
}
