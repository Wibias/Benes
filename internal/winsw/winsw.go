package winsw

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const Version = "2.12.0"
const URL = "https://github.com/winsw/winsw/releases/download/v2.12.0/WinSW.NET461.exe"
const SHA256 = "b5066b7bbdfba1293e5d15cda3caaea88fbeab35bd5b38c41c913d492aadfc4f"
const ServiceID = "benes-proxy-native"

type Status string

const (
	StatusStarted     Status = "started"
	StatusStopped     Status = "stopped"
	StatusNonexistent Status = "nonexistent"
	StatusUnknown     Status = "unknown"
)

func Dir(home string) string {
	return filepath.Join(home, "winsw")
}

func ExePath(home string) string {
	return filepath.Join(Dir(home), ServiceID+".exe")
}

func XMLPath(home string) string {
	return filepath.Join(Dir(home), ServiceID+".xml")
}

func ParseStatus(output string) Status {
	normalized := strings.ToLower(strings.TrimSpace(output))
	if strings.Contains(normalized, "nonexistent") {
		return StatusNonexistent
	}
	if strings.Contains(normalized, "started") {
		return StatusStarted
	}
	if strings.Contains(normalized, "stopped") {
		return StatusStopped
	}
	return StatusUnknown
}

func VerifySHA256(data []byte) error {
	sum := sha256.Sum256(data)
	got := hex.EncodeToString(sum[:])
	if got != SHA256 {
		return fmt.Errorf("WinSW download failed SHA-256 verification (got %s, expected %s)", got, SHA256)
	}
	return nil
}

func BuildXML(benesExe, home string, port int, env map[string]string) string {
	if port <= 0 || port > 65535 {
		port = 23100
	}
	if env == nil {
		env = map[string]string{}
	}
	domain := strings.TrimSpace(env["USERDOMAIN"])
	if domain == "" {
		domain = "."
	}
	user := strings.TrimSpace(env["USERNAME"])
	lines := []string{
		`  <env name="BENES_SERVICE" value="1"/>`,
		`  <env name="BENES_HOME" value="` + xmlEscape(home) + `"/>`,
		`  <env name="BENES_HOME" value="` + xmlEscape(home) + `"/>`,
		`  <env name="PATH" value="` + xmlEscape(env["PATH"]) + `"/>`,
	}
	if v := strings.TrimSpace(env["CODEX_HOME"]); v != "" {
		lines = append(lines, `  <env name="CODEX_HOME" value="`+xmlEscape(v)+`"/>`)
	}
	return `<?xml version="1.0" encoding="UTF-8"?>
<service>
  <id>` + ServiceID + `</id>
  <name>Benes Proxy (native)</name>
  <description>Benes proxy running as a native Windows service.</description>
  <executable>` + xmlEscape(benesExe) + `</executable>
  <arguments>start --port ` + strconv.Itoa(port) + `</arguments>
` + strings.Join(lines, "\n") + `
  <logpath>` + xmlEscape(home) + `</logpath>
  <log mode="roll-by-size">
    <sizeThreshold>10240</sizeThreshold>
    <keepFiles>4</keepFiles>
  </log>
  <onfailure action="restart" delay="5 sec"/>
  <stoptimeout>20 sec</stoptimeout>
  <serviceaccount>
    <domain>` + xmlEscape(domain) + `</domain>
    <user>` + xmlEscape(user) + `</user>
    <allowservicelogon>true</allowservicelogon>
  </serviceaccount>
</service>
`
}

func xmlEscape(value string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;")
	return r.Replace(value)
}

func LocalSystemStartName(qc string) bool {
	match := ""
	for _, line := range strings.Split(qc, "\n") {
		lower := strings.ToLower(line)
		if strings.Contains(lower, "service_start_name") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				match = strings.TrimSpace(parts[1])
			}
		}
	}
	return strings.Contains(strings.ToLower(match), "localsystem")
}

func DefaultUser(env map[string]string) string {
	if env == nil {
		env = map[string]string{}
	}
	return strings.TrimSpace(env["USERNAME"])
}

func StartNameMatchesUser(qc, user string) bool {
	user = strings.TrimSpace(user)
	if user == "" {
		return true
	}
	return strings.Contains(strings.ToLower(qc), strings.ToLower(user))
}

func MustExist(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
