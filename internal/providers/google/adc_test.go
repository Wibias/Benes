package google

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type adcRoundTrip func(*http.Request) (*http.Response, error)

func (f adcRoundTrip) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func adcJSONResponse(status int, body any) *http.Response {
	raw, _ := json.Marshal(body)
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewReader(raw)),
	}
}

func writeADCFile(t *testing.T, raw string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "adc.json")
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestAccessTokenExchangesAuthorizedUserWithoutLeakingRefresh(t *testing.T) {
	globalADC = adcCache{}
	path := writeADCFile(t, `{"type":"authorized_user","client_id":"cid","client_secret":"csec","refresh_token":"rt-secret"}`)
	var gotBody string
	var calls int
	client := &http.Client{Transport: adcRoundTrip(func(req *http.Request) (*http.Response, error) {
		calls++
		raw, _ := io.ReadAll(req.Body)
		gotBody = string(raw)
		if req.URL.String() != oauthTokenURL {
			t.Fatalf("url=%s", req.URL)
		}
		return adcJSONResponse(200, map[string]any{"access_token": "ya29.tok", "expires_in": 3600}), nil
	})}
	tok, err := AccessToken(context.Background(), ADCEnv{
		Environ: map[string]string{"GOOGLE_APPLICATION_CREDENTIALS": path},
		HTTP:    client,
		Now:     func() time.Time { return time.Unix(1_700_000_000, 0) },
		Sleep:   func(time.Duration) {},
	})
	if err != nil || tok != "ya29.tok" {
		t.Fatalf("tok=%q err=%v", tok, err)
	}
	if !strings.Contains(gotBody, "rt-secret") || !strings.Contains(gotBody, "grant_type=refresh_token") {
		t.Fatalf("body=%s", gotBody)
	}
	if _, err := AccessToken(context.Background(), ADCEnv{
		Environ: map[string]string{"GOOGLE_APPLICATION_CREDENTIALS": path},
		HTTP: &http.Client{Transport: adcRoundTrip(func(req *http.Request) (*http.Response, error) {
			t.Fatal("cache should prevent second exchange")
			return nil, nil
		})},
		Now:   func() time.Time { return time.Unix(1_700_000_000, 0) },
		Sleep: func(time.Duration) {},
	}); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("calls=%d", calls)
	}
}

func TestAccessTokenFailsClosedOnMissingFile(t *testing.T) {
	globalADC = adcCache{}
	_, err := AccessToken(context.Background(), ADCEnv{
		Environ: map[string]string{"GOOGLE_APPLICATION_CREDENTIALS": filepath.Join(t.TempDir(), "missing.json")},
		Sleep:   func(time.Duration) {},
	})
	if err == nil || !strings.Contains(err.Error(), "Application Default Credentials") || strings.Contains(err.Error(), "rt-") {
		t.Fatalf("err=%v", err)
	}
}

func TestAccessTokenRejectsHTTPErrorWithoutLeakingSecrets(t *testing.T) {
	globalADC = adcCache{}
	path := writeADCFile(t, `{"type":"authorized_user","client_id":"cid","client_secret":"csec","refresh_token":"rt-secret"}`)
	_, err := AccessToken(context.Background(), ADCEnv{
		Environ: map[string]string{"GOOGLE_APPLICATION_CREDENTIALS": path},
		HTTP: &http.Client{Transport: adcRoundTrip(func(req *http.Request) (*http.Response, error) {
			return adcJSONResponse(400, map[string]any{"error": "invalid_grant", "error_description": "rt-secret leaked"}), nil
		})},
		Sleep: func(time.Duration) {},
	})
	if err == nil || strings.Contains(err.Error(), "rt-secret") || strings.Contains(err.Error(), "invalid_grant") {
		t.Fatalf("err=%v", err)
	}
}

func TestAccessTokenExchangesServiceAccountJWT(t *testing.T) {
	globalADC = adcCache{}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	pemKey := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	raw, _ := json.Marshal(map[string]any{
		"type":           "service_account",
		"client_email":   "sa@example.iam.gserviceaccount.com",
		"private_key":    string(pemKey),
		"private_key_id": "kid-1",
	})
	path := writeADCFile(t, string(raw))
	var gotBody string
	tok, err := AccessToken(context.Background(), ADCEnv{
		Environ: map[string]string{"GOOGLE_APPLICATION_CREDENTIALS": path},
		HTTP: &http.Client{Transport: adcRoundTrip(func(req *http.Request) (*http.Response, error) {
			body, _ := io.ReadAll(req.Body)
			gotBody = string(body)
			return adcJSONResponse(200, map[string]any{"access_token": "ya29.sa", "expires_in": 3600}), nil
		})},
		Now:   func() time.Time { return time.Unix(1_700_000_000, 0) },
		Sleep: func(time.Duration) {},
	})
	if err != nil || tok != "ya29.sa" {
		t.Fatalf("tok=%q err=%v", tok, err)
	}
	if !strings.Contains(gotBody, "urn%3Aietf%3Aparams%3Aoauth%3Agrant-type%3Ajwt-bearer") && !strings.Contains(gotBody, "jwt-bearer") {
		t.Fatalf("body=%s", gotBody)
	}
	if !strings.Contains(gotBody, "assertion=") {
		t.Fatalf("missing assertion body=%s", gotBody)
	}
}

func TestAccessTokenFallsBackToMetadataServer(t *testing.T) {
	globalADC = adcCache{}
	var flavor string
	tok, err := AccessToken(context.Background(), ADCEnv{
		Home:     t.TempDir(),
		Environ:  map[string]string{"CLOUDSDK_CONFIG": t.TempDir()},
		Metadata: "http://metadata.test/token",
		HTTP: &http.Client{Transport: adcRoundTrip(func(req *http.Request) (*http.Response, error) {
			if req.Method != http.MethodGet || req.URL.String() != "http://metadata.test/token" {
				t.Fatalf("req=%s %s", req.Method, req.URL)
			}
			flavor = req.Header.Get("Metadata-Flavor")
			return adcJSONResponse(200, map[string]any{"access_token": "ya29.meta", "expires_in": 3600}), nil
		})},
		Now:   func() time.Time { return time.Unix(1_700_000_000, 0) },
		Sleep: func(time.Duration) {},
	})
	if err != nil || tok != "ya29.meta" || flavor != "Google" {
		t.Fatalf("tok=%q flavor=%q err=%v", tok, flavor, err)
	}
}

func TestAccessTokenRejectsUnsupportedType(t *testing.T) {
	globalADC = adcCache{}
	path := writeADCFile(t, `{"type":"external_account","audience":"//iam.googleapis.com/x"}`)
	_, err := AccessToken(context.Background(), ADCEnv{
		Environ: map[string]string{"GOOGLE_APPLICATION_CREDENTIALS": path},
		Sleep:   func(time.Duration) {},
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("err=%v", err)
	}
}

func TestAccessTokenRetriesTransientTokenHTTPThenSucceeds(t *testing.T) {
	globalADC = adcCache{}
	path := writeADCFile(t, `{"type":"authorized_user","client_id":"cid","client_secret":"csec","refresh_token":"rt"}`)
	var calls int
	tok, err := AccessToken(context.Background(), ADCEnv{
		Environ: map[string]string{"GOOGLE_APPLICATION_CREDENTIALS": path},
		HTTP: &http.Client{Transport: adcRoundTrip(func(req *http.Request) (*http.Response, error) {
			calls++
			if calls < 3 {
				return adcJSONResponse(503, map[string]any{"error": "unavailable"}), nil
			}
			return adcJSONResponse(200, map[string]any{"access_token": "ya29.retry", "expires_in": 3600}), nil
		})},
		Now:   func() time.Time { return time.Unix(1_700_000_000, 0) },
		Sleep: func(time.Duration) {},
	})
	if err != nil || tok != "ya29.retry" || calls != 3 {
		t.Fatalf("tok=%q calls=%d err=%v", tok, calls, err)
	}
}
