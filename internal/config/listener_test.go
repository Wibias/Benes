package config

import (
	"os"
	"strconv"
	"testing"
)

func loadListener(t *testing.T, body string) (DiskConfig, ListenerConfig, error) {
	t.Helper()
	path := writeListenerFixture(t, body)
	disk, err := LoadDiskConfig(path, 1<<20)
	if err != nil {
		return DiskConfig{}, ListenerConfig{}, err
	}
	listener, err := ProjectListener(disk)
	return disk, listener, err
}
func writeListenerFixture(t *testing.T, body string) string {
	t.Helper()
	path := t.TempDir() + "/config.json"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
func TestProjectListenerDefaults(t *testing.T) {
	_, got, err := loadListener(t, `{"providers":{}}`)
	if err != nil {
		t.Fatal(err)
	}
	if got.Hostname != DefaultListenerHostname || got.Port != DefaultListenerPort {
		t.Fatalf("got=%+v", got)
	}
	if DefaultListenerPort != 23100 || DefaultDevPort != 23200 {
		t.Fatalf("listener=%d dev=%d", DefaultListenerPort, DefaultDevPort)
	}
}
func TestProjectListenerCanonicalizesLocalhostAndWhitespace(t *testing.T) {
	for _, host := range []string{"localhost", "LOCALHOST", " localhost "} {
		_, got, err := loadListener(t, `{"providers":{},"hostname":`+quote(host)+`,"port":20200}`)
		if err != nil {
			t.Fatal(err)
		}
		if got.Hostname != "127.0.0.1" || got.Port != 20200 {
			t.Fatalf("host=%q got=%+v", host, got)
		}
	}
	_, got, err := loadListener(t, `{"providers":{},"hostname":"  "}`)
	if err != nil || got.Hostname != "127.0.0.1" {
		t.Fatalf("got=%+v err=%v", got, err)
	}
}
func TestProjectListenerPreservesExplicitBindHost(t *testing.T) {
	for _, host := range []string{"0.0.0.0", "::", "192.0.2.4", "gateway.internal"} {
		_, got, err := loadListener(t, `{"providers":{},"hostname":`+quote(host)+`}`)
		if err != nil {
			t.Fatal(err)
		}
		if got.Hostname != host {
			t.Fatalf("host=%q got=%q", host, got.Hostname)
		}
	}
}
func TestProjectListenerAcceptsJavaScriptIntegerNumberForms(t *testing.T) {
	for _, literal := range []string{"23100", "23100.0", "2.31e4", "65535"} {
		_, got, err := loadListener(t, `{"providers":{},"port":`+literal+`}`)
		if err != nil {
			t.Fatalf("literal=%s err=%v", literal, err)
		}
		want := 23100
		if literal == "65535" {
			want = 65535
		}
		if got.Port != want {
			t.Fatalf("literal=%s port=%d", literal, got.Port)
		}
	}
}
func TestProjectListenerRejectsInvalidPersistedPorts(t *testing.T) {
	for _, literal := range []string{"0", "-1", "65536", "1.5", "null", "\"23100\"", "true", "{}", "[]"} {
		_, _, err := loadListener(t, `{"providers":{},"port":`+literal+`}`)
		if err == nil {
			t.Fatalf("accepted port %s", literal)
		}
	}
}
func TestProjectListenerRejectsInvalidHostnameTypes(t *testing.T) {
	for _, literal := range []string{"null", "23100", "true", "{}", "[]"} {
		_, _, err := loadListener(t, `{"providers":{},"hostname":`+literal+`}`)
		if err == nil {
			t.Fatalf("accepted hostname %s", literal)
		}
	}
}
func TestProjectListenerRejectsSyntheticDiskConfigWithoutRaw(t *testing.T) {
	if _, err := ProjectListener(DiskConfig{}); err == nil {
		t.Fatal("accepted empty Raw")
	}
}
func quote(s string) string { return strconv.Quote(s) }
