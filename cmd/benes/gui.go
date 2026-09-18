package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Wibias/Benes/internal/config"
)

func runGUI(args []string, stdout, stderr io.Writer, deps commandDependencies) int {
	if len(args) > 0 {
		fmt.Fprintln(stderr, "benes: gui does not accept arguments")
		return 2
	}
	paths, err := deps.resolvePaths(config.PathOptions{})
	if err != nil {
		fmt.Fprintf(stderr, "benes: resolve paths: %v\n", err)
		return 1
	}
	state, err := readRuntimePortState(paths.RuntimePort)
	if !evaluateRuntime(state, err, deps).Running() {
		fmt.Fprintln(stdout, "Proxy not running. Starting...")
		spawn := deps.spawnStart
		if spawn == nil {
			spawn = defaultSpawnStart
		}
		if err := spawn(); err != nil {
			fmt.Fprintf(stderr, "benes: start proxy: %v\n", err)
			return 1
		}
		sleep := deps.sleep
		if sleep == nil {
			sleep = time.Sleep
		}
		state = runtimePortState{}
		for i := 0; i < 40; i++ {
			next, readErr := readRuntimePortState(paths.RuntimePort)
			if evaluateRuntime(next, readErr, deps).Running() {
				state = next
				break
			}
			sleep(50 * time.Millisecond)
		}
		if state.PID <= 0 || state.Port <= 0 {
			fmt.Fprintln(stderr, "benes: proxy did not become healthy after starting. Not opening the GUI.")
			return 1
		}
	}
	host := strings.TrimSpace(state.Hostname)
	if host == "" || host == "127.0.0.1" {
		host = "localhost"
	}
	guiURL := fmt.Sprintf("http://%s:%d", host, state.Port)
	fmt.Fprintf(stdout, "Opening %s\n", guiURL)
	open := deps.openURL
	if open == nil {
		open = defaultOpenURL
	}
	if err := open(guiURL); err != nil {
		fmt.Fprintf(stderr, "benes: open GUI: %v\n", err)
		return 1
	}
	return 0
}

func defaultSpawnStart() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, "start")
	cmd.Stdin = nil
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

func defaultOpenURL(rawURL string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command(windowsRundll32(), "url.dll,FileProtocolHandler", rawURL)
	case "darwin":
		cmd = exec.Command("open", rawURL)
	default:
		cmd = exec.Command("xdg-open", rawURL)
	}
	cmd.Stdin = nil
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

func windowsRundll32() string {
	root := os.Getenv("SystemRoot")
	if root == "" {
		root = os.Getenv("WINDIR")
	}
	if root == "" {
		root = `C:\Windows`
	}
	candidate := filepath.Join(root, "System32", "rundll32.exe")
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	return "rundll32"
}
