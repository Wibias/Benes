package codexappserver

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

func hostGOOS() string { return runtime.GOOS }

func hostUID() *int {
	uid := os.Getuid()
	if uid < 0 {
		return nil
	}
	return &uid
}

func defaultListSnapshots(platform string, uid *int) ([]Snapshot, error) {
	if isWindows(platform) {
		return listWindowsSnapshots()
	}
	if platform == "darwin" {
		return listDarwinSnapshots(uid)
	}
	return listUnixProcSnapshots(uid)
}

func readProcessStartMsBatch(pids []int, platform string) map[int]*int64 {
	out := map[int]*int64{}
	if len(pids) == 0 {
		return out
	}
	if platform == "darwin" {
		return readDarwinStartMsBatch(pids)
	}
	if isWindows(platform) {
		return readWindowsStartMsBatch(pids)
	}
	for _, pid := range pids {
		out[pid] = readLinuxProcStartMs(pid)
	}
	return out
}

func listUnixProcSnapshots(uid *int) ([]Snapshot, error) {
	if _, err := os.Stat("/proc"); err != nil {
		return nil, fmt.Errorf("procfs_unavailable")
	}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	out := make([]Snapshot, 0)
	for _, ent := range entries {
		name := ent.Name()
		pid, conv := strconv.Atoi(name)
		if conv != nil || pid <= 1 {
			continue
		}
		statusRaw, err := os.ReadFile("/proc/" + name + "/status")
		if err != nil {
			continue
		}
		processUID := parseUnixProcStatusUID(string(statusRaw))
		if uid != nil && processUID != nil && *processUID != *uid {
			continue
		}
		cmdline, err := os.ReadFile("/proc/" + name + "/cmdline")
		if err != nil {
			continue
		}
		argv := strings.Split(strings.TrimRight(string(cmdline), "\x00"), "\x00")
		filtered := make([]string, 0, len(argv))
		for _, arg := range argv {
			if arg != "" {
				filtered = append(filtered, arg)
			}
		}
		commandLine := strings.TrimSpace(strings.Join(filtered, " "))
		if commandLine == "" {
			continue
		}
		executable := ""
		if len(filtered) > 0 {
			executable = filtered[0]
		}
		out = append(out, Snapshot{PID: pid, CommandLine: commandLine, Executable: executable, UID: processUID})
	}
	return out, nil
}

func parseUnixProcStatusUID(status string) *int {
	for _, line := range strings.Split(status, "\n") {
		if !strings.HasPrefix(line, "Uid:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return nil
		}
		uid, err := strconv.Atoi(fields[1])
		if err != nil {
			return nil
		}
		return &uid
	}
	return nil
}

func readLinuxProcStartMs(pid int) *int64 {
	stat, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return nil
	}
	text := string(stat)
	closeParen := strings.LastIndex(text, ")")
	if closeParen < 0 {
		return nil
	}
	fields := strings.Fields(text[closeParen+2:])
	if len(fields) < 20 {
		return nil
	}
	startTicks, err := strconv.ParseFloat(fields[19], 64)
	if err != nil {
		return nil
	}
	bootRaw, err := os.ReadFile("/proc/stat")
	if err != nil {
		return nil
	}
	var boot int64
	found := false
	for _, line := range strings.Split(string(bootRaw), "\n") {
		if strings.HasPrefix(line, "btime ") {
			boot, err = strconv.ParseInt(strings.TrimSpace(strings.TrimPrefix(line, "btime ")), 10, 64)
			if err != nil {
				return nil
			}
			found = true
			break
		}
	}
	if !found {
		return nil
	}
	const hertz = 100
	ms := (float64(boot) + startTicks/hertz) * 1000
	value := int64(ms)
	return &value
}

func listDarwinSnapshots(uid *int) ([]Snapshot, error) {
	var commandArgs []string
	var executableArgs []string
	if uid != nil {
		commandArgs = []string{"-u", strconv.Itoa(*uid), "-o", "pid=,command="}
		executableArgs = []string{"-u", strconv.Itoa(*uid), "-o", "pid=,comm="}
	} else {
		commandArgs = []string{"-axo", "pid=,uid=,command="}
		executableArgs = []string{"-axo", "pid=,comm="}
	}
	commandOutput, err := execFileOutput("/bin/ps", commandArgs, 5*time.Second)
	if err != nil {
		return nil, err
	}
	executableOutput, err := execFileOutput("/bin/ps", executableArgs, 5*time.Second)
	if err != nil {
		return nil, err
	}
	executableByPID := map[int]string{}
	for _, raw := range strings.Split(executableOutput, "\n") {
		fields := strings.TrimSpace(raw)
		if fields == "" {
			continue
		}
		pidStr, rest, ok := strings.Cut(strings.TrimLeft(fields, " "), " ")
		if !ok {
			continue
		}
		pid, conv := strconv.Atoi(pidStr)
		if conv != nil || pid <= 0 {
			continue
		}
		executable := strings.TrimSpace(rest)
		if executable != "" {
			executableByPID[pid] = executable
		}
	}
	out := make([]Snapshot, 0)
	for _, raw := range strings.Split(commandOutput, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if uid != nil {
			pidStr, rest, ok := strings.Cut(line, " ")
			if !ok {
				continue
			}
			pid, conv := strconv.Atoi(pidStr)
			commandLine := strings.TrimSpace(rest)
			if conv != nil || pid <= 0 || commandLine == "" {
				continue
			}
			out = append(out, Snapshot{PID: pid, CommandLine: commandLine, Executable: executableByPID[pid], UID: uid})
			continue
		}
		parts := strings.SplitN(line, " ", 3)
		if len(parts) < 3 {
			continue
		}
		pid, conv := strconv.Atoi(parts[0])
		processUID, uidConv := strconv.Atoi(parts[1])
		commandLine := strings.TrimSpace(parts[2])
		if conv != nil || pid <= 0 || commandLine == "" {
			continue
		}
		var uidPtr *int
		if uidConv == nil {
			uidPtr = &processUID
		}
		out = append(out, Snapshot{PID: pid, CommandLine: commandLine, Executable: executableByPID[pid], UID: uidPtr})
	}
	return out, nil
}

func readDarwinStartMsBatch(pids []int) map[int]*int64 {
	out := map[int]*int64{}
	pidArgs := make([]string, 0, len(pids))
	for _, pid := range pids {
		pidArgs = append(pidArgs, strconv.Itoa(pid))
	}
	stdout, err := execFileOutput("/bin/ps", []string{"-o", "pid=,lstart=", "-p", strings.Join(pidArgs, ",")}, 3*time.Second)
	if err != nil {
		for _, pid := range pids {
			out[pid] = nil
		}
		return out
	}
	byPID := map[int]int64{}
	for _, raw := range strings.Split(stdout, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		pidStr, rest, ok := strings.Cut(line, " ")
		if !ok {
			continue
		}
		pid, conv := strconv.Atoi(pidStr)
		if conv != nil {
			continue
		}
		parsed, perr := time.Parse(time.ANSIC, strings.TrimSpace(rest))
		if perr != nil {
			parsed, perr = time.Parse("Mon Jan 2 15:04:05 2006", strings.TrimSpace(rest))
		}
		if perr != nil {
			continue
		}
		byPID[pid] = parsed.UnixMilli()
	}
	for _, pid := range pids {
		if ms, ok := byPID[pid]; ok {
			value := ms
			out[pid] = &value
		} else {
			out[pid] = nil
		}
	}
	return out
}

func powerShellSingleQuotedIgnoreCaseMatch(patternSource string) string {
	return "'(?i)" + strings.ReplaceAll(patternSource, "'", "''") + "'"
}

// WindowsCIMScript is the GetOwner-scoped CIM enumerator. Tests pin Invoke-CimMethod
// and the incomplete-owner marker so a rewrite cannot fail open.
func WindowsCIMScript() string {
	basenameMatch := powerShellSingleQuotedIgnoreCaseMatch(WindowsBasenameCandidateSource)
	codeModeMatch := powerShellSingleQuotedIgnoreCaseMatch(WindowsCodeModeHostCandidateSource)
	return strings.Join([]string{
		"$ErrorActionPreference='SilentlyContinue'",
		"$me=[System.Security.Principal.WindowsIdentity]::GetCurrent().Name",
		"Get-CimInstance Win32_Process | Where-Object {",
		"  -not [string]::IsNullOrWhiteSpace($_.CommandLine) -and (",
		"    $_.CommandLine -match " + basenameMatch + " -or",
		"    $_.CommandLine -match " + codeModeMatch,
		"  )",
		"} | ForEach-Object {",
		"  try {",
		"    $o=Invoke-CimMethod -InputObject $_ -MethodName GetOwner -ErrorAction Stop",
		"    if($null -eq $o -or $o.ReturnValue -ne 0 -or [string]::IsNullOrWhiteSpace($o.User)){\"__BENES_ENUM_INCOMPLETE__\"; return}",
		"    $owner=if($o.Domain){\"$($o.Domain)\\$($o.User)\"}else{$o.User}",
		"    if($owner -ine $me){return}",
		"    $cmd=($_.CommandLine -replace \"`t\",\" \")",
		"    \"{0}`t{1}`t{2}\" -f $_.ProcessId, $cmd, $owner",
		"  } catch { \"__BENES_ENUM_INCOMPLETE__\" }",
		"}",
	}, "\n")
}

func listWindowsSnapshots() ([]Snapshot, error) {
	output, err := execFileOutput(trustedPowerShellPath(), []string{
		"-NoProfile", "-NoLogo", "-NonInteractive", "-Command", WindowsCIMScript(),
	}, 8*time.Second)
	if err != nil {
		return nil, err
	}
	out := make([]Snapshot, 0)
	for _, line := range strings.Split(output, "\n") {
		if strings.TrimSpace(line) == "__BENES_ENUM_INCOMPLETE__" {
			return nil, fmt.Errorf("windows_enum_incomplete")
		}
		tab := strings.IndexByte(line, '\t')
		if tab <= 0 {
			continue
		}
		tab2 := strings.IndexByte(line[tab+1:], '\t')
		if tab2 < 0 {
			continue
		}
		tab2 += tab + 1
		pid, conv := strconv.Atoi(line[:tab])
		commandLine := strings.TrimSpace(line[tab+1 : tab2])
		owner := strings.TrimSpace(line[tab2+1:])
		if conv != nil || pid <= 1 || commandLine == "" || owner == "" {
			continue
		}
		out = append(out, Snapshot{PID: pid, CommandLine: commandLine, Owner: owner})
	}
	return out, nil
}

func readWindowsStartMsBatch(pids []int) map[int]*int64 {
	out := map[int]*int64{}
	filters := make([]string, 0, len(pids))
	for _, pid := range pids {
		filters = append(filters, "ProcessId="+strconv.Itoa(pid))
	}
	script := `Get-CimInstance Win32_Process -Filter "` + strings.Join(filters, " OR ") + `" | ForEach-Object { "$($_.ProcessId)` + "\t" + `$($_.CreationDate.ToUniversalTime().ToString("o"))" }`
	stdout, err := execFileOutput(trustedPowerShellPath(), []string{
		"-NoProfile", "-NoLogo", "-NonInteractive", "-Command", script,
	}, 5*time.Second)
	if err != nil {
		for _, pid := range pids {
			out[pid] = nil
		}
		return out
	}
	byPID := map[int]int64{}
	for _, line := range strings.Split(stdout, "\n") {
		tab := strings.IndexByte(line, '\t')
		if tab <= 0 {
			continue
		}
		pid, conv := strconv.Atoi(line[:tab])
		parsed, perr := time.Parse(time.RFC3339Nano, strings.TrimSpace(line[tab+1:]))
		if conv != nil || perr != nil {
			continue
		}
		byPID[pid] = parsed.UnixMilli()
	}
	for _, pid := range pids {
		if ms, ok := byPID[pid]; ok {
			value := ms
			out[pid] = &value
		} else {
			out[pid] = nil
		}
	}
	return out
}

func trustedPowerShellPath() string {
	return filepath.Join(windowsSystemDir(), "WindowsPowerShell", "v1.0", "powershell.exe")
}

func trustedTaskkillPath() string {
	return filepath.Join(windowsSystemDir(), "taskkill.exe")
}

func fallbackSystemDir() string {
	root := os.Getenv("SystemRoot")
	if root == "" {
		root = `C:\Windows`
	}
	return filepath.Join(root, "System32")
}

func execFileOutput(file string, args []string, timeout time.Duration) (string, error) {
	cmd := exec.Command(file, args...)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = nil
	cmd.Stdin = nil
	hideWindow(cmd)
	if err := cmd.Start(); err != nil {
		return "", err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return stdout.String(), err
	case <-time.After(timeout):
		_ = cmd.Process.Kill()
		<-done
		return "", fmt.Errorf("timeout running %s", file)
	}
}

func execTrusted(file string, args []string) error {
	cmd := exec.Command(file, args...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Stdin = nil
	hideWindow(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		<-done
		return fmt.Errorf("timeout running %s", file)
	}
}
