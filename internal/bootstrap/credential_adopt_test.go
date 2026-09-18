package bootstrap

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/config"
	"github.com/Wibias/Benes/internal/credentials"
)

func TestBuildDataPlaneAdoptsPlaintextAPIKeyIntoCredentialStore(t *testing.T) {
	upstream := newResponsesUpstream(t)
	defer upstream.Close()
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	body := fmt.Sprintf(`{"hostname":"127.0.0.1","port":23100,"providers":{"local":{"adapter":"openai-responses","baseUrl":%q,"apiKey":"provider-key","allowPrivateNetwork":true}}}`, upstream.URL)
	if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	disk, err := config.LoadDiskConfig(configPath, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	plane, err := BuildDataPlane(t.Context(), disk, DataPlaneOptions{
		ConfigPath: configPath,
		CodexPool:  CodexPoolOptions{BenesHome: home},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = plane.Close() })
	if plane.Credentials == nil {
		t.Fatal("credential store missing")
	}
	secret, err := plane.Credentials.Get(credentials.Ref{ID: "local", Source: credentials.SourceSecureStore})
	if err != nil {
		t.Fatal(err)
	}
	if string(secret) != "provider-key" {
		t.Fatalf("secret=%q", secret)
	}
	committed, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(committed), "provider-key") {
		t.Fatalf("plaintext remained: %s", committed)
	}
	if !strings.Contains(string(committed), `"credentialRef"`) {
		t.Fatalf("ref missing: %s", committed)
	}
}

func TestBuildDataPlaneHandlerPOSTPublishesCredentialRef(t *testing.T) {
	upstream := newResponsesUpstream(t)
	defer upstream.Close()
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	body := fmt.Sprintf(`{"hostname":"127.0.0.1","port":23100,"providers":{"local":{"adapter":"openai-responses","baseUrl":%q,"apiKey":"provider-key","allowPrivateNetwork":true}}}`, upstream.URL)
	if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	disk, err := config.LoadDiskConfig(configPath, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	plane, err := BuildDataPlane(t.Context(), disk, DataPlaneOptions{
		ConfigPath: configPath,
		CodexPool:  CodexPoolOptions{BenesHome: home},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = plane.Close() })
	req, err := http.NewRequest(http.MethodPost, "/api/credentials", strings.NewReader(`{"id":"local","secret":"sk-new"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Host = "127.0.0.1"
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	plane.Handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	committed, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	got := string(committed)
	if strings.Contains(got, "sk-new") || strings.Contains(got, "provider-key") {
		t.Fatalf("plaintext remained: %s", got)
	}
	if !strings.Contains(got, `"credentialRef"`) {
		t.Fatalf("ref missing: %s", got)
	}
}
