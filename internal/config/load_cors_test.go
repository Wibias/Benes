package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func writeCORSConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadDiskConfigProjectsExactCORSAllowOrigins(t *testing.T) {
	path := writeCORSConfig(t, `{"providers":{},"corsAllowOrigins":["https://Example.com:443/path","chrome-extension://ABCDEF/options.html","null"]}`)
	cfg, err := LoadDiskConfig(path, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"https://Example.com:443/path", "chrome-extension://ABCDEF/options.html", "null"}
	if !reflect.DeepEqual(cfg.CORSAllowOrigins, want) {
		t.Fatalf("origins=%#v want=%#v", cfg.CORSAllowOrigins, want)
	}
}

func TestLoadDiskConfigTreatsMissingAndNullCORSAllowOriginsAsAbsent(t *testing.T) {
	for _, content := range []string{`{"providers":{}}`, `{"providers":{},"corsAllowOrigins":null}`} {
		cfg, err := LoadDiskConfig(writeCORSConfig(t, content), 1<<20)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.CORSAllowOrigins != nil {
			t.Fatalf("origins=%#v", cfg.CORSAllowOrigins)
		}
	}
}

func TestLoadDiskConfigRejectsMalformedCORSAllowOrigins(t *testing.T) {
	for _, value := range []string{`"https://example.com"`, `{}`, `1`, `true`, `["https://ok",""]`, `["https://ok","   "]`, `["https://ok",7]`} {
		path := writeCORSConfig(t, `{"providers":{},"corsAllowOrigins":`+value+`}`)
		if _, err := LoadDiskConfig(path, 1<<20); err == nil {
			t.Fatalf("accepted corsAllowOrigins=%s", value)
		}
	}
}
