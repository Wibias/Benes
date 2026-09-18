//go:build windows

package transport

import (
	"os/exec"
	"strings"
)

func lookupSystemProxy() (string, []string, error) {
	out, err := exec.Command("reg", "query", `HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings`).CombinedOutput()
	if err != nil {
		return "", nil, err
	}
	text := string(out)
	if !registryDWORDEnabled(text, "ProxyEnable") {
		return "", splitNoProxy(registryValue(text, "ProxyOverride")), nil
	}
	server := registryValue(text, "ProxyServer")
	if server == "" {
		return "", splitNoProxy(registryValue(text, "ProxyOverride")), nil
	}
	if !strings.Contains(server, "://") {
		server = "http://" + server
	}
	return server, splitNoProxy(strings.ReplaceAll(registryValue(text, "ProxyOverride"), ";", ",")), nil
}

func registryDWORDEnabled(text, name string) bool {
	value := registryValue(text, name)
	return strings.EqualFold(value, "0x1") || value == "1"
}

func registryValue(text, name string) string {
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 3 || !strings.EqualFold(fields[0], name) {
			continue
		}
		return strings.TrimSpace(strings.Join(fields[2:], " "))
	}
	return ""
}
