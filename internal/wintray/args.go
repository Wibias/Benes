package wintray

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func PowerShellPath() string {
	root := os.Getenv("SystemRoot")
	if root == "" {
		root = `C:\Windows`
	}
	return filepath.Join(root, "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
}

func WScriptPath() string {
	root := os.Getenv("SystemRoot")
	if root == "" {
		root = `C:\Windows`
	}
	return filepath.Join(root, "System32", "wscript.exe")
}

type Entry struct {
	CLI       string
	Script    string
	CodexHome string
	BenesHome string
}

func ProcessArgs(entry Entry, mode string, hostPID int) ([]string, error) {
	if mode == "" {
		mode = "Run"
	}
	for _, path := range []string{entry.Script, entry.CLI, entry.CodexHome, entry.BenesHome} {
		if _, err := Quote(path); err != nil {
			return nil, err
		}
	}
	args := []string{
		"-NoLogo", "-NoProfile", "-NonInteractive", "-STA",
		"-ExecutionPolicy", "Bypass",
		"-File", entry.Script,
		"-CliPath", entry.CLI,
		"-CodexHome", entry.CodexHome,
		"-BenesHome", entry.BenesHome,
		"-Mode", mode,
	}
	if hostPID > 0 {
		args = append(args, "-HostPid", fmt.Sprintf("%d", hostPID))
	}
	return args, nil
}

func PowerShellCommand(entry Entry) (string, error) {
	ps, err := Quote(PowerShellPath())
	if err != nil {
		return "", err
	}
	args, err := ProcessArgs(entry, "Run", 0)
	if err != nil {
		return "", err
	}
	parts := []string{ps}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "-") {
			parts = append(parts, arg)
			continue
		}
		quoted, err := Quote(arg)
		if err != nil {
			return "", err
		}
		parts = append(parts, quoted)
	}
	return strings.Join(parts, " "), nil
}

func RunCommand(launcherPath string) (string, error) {
	wscript, err := Quote(WScriptPath())
	if err != nil {
		return "", err
	}
	launcher, err := Quote(launcherPath)
	if err != nil {
		return "", err
	}
	cmd := wscript + " //B //NoLogo " + launcher
	if len(cmd) > 260 {
		return "", fmt.Errorf("tray Run command exceeds the Windows 260-character limit (%d chars)", len(cmd))
	}
	return cmd, nil
}

func LauncherScript(entry Entry) (string, error) {
	command, err := PowerShellCommand(entry)
	if err != nil {
		return "", err
	}
	escaped := strings.ReplaceAll(command, `"`, `""`)
	return "' Benes owned tray launcher — do not edit by hand.\r\n" +
		`CreateObject("WScript.Shell").Run "` + escaped + `", 0, False` + "\r\n", nil
}
