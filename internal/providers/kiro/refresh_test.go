package kiro

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type refreshRoundTrip func(*http.Request) (*http.Response, error)

func (f refreshRoundTrip) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func jsonResponse(status int, body any) *http.Response {
	raw, _ := json.Marshal(body)
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewReader(raw)),
	}
}

func TestRefreshDesktopTokenUsesAccountRegion(t *testing.T) {
	var gotURL, gotBody string
	out, err := RefreshToken(context.Background(), RefreshInput{
		Refresh: "rt-keep",
		Stored: &RefreshAccount{
			SSORegion:  "eu-west-1",
			ProfileARN: "arn:aws:codewhisperer:eu-west-1:123456789012:profile/a",
		},
		HTTPClient: &http.Client{Transport: refreshRoundTrip(func(req *http.Request) (*http.Response, error) {
			raw, _ := io.ReadAll(req.Body)
			gotURL, gotBody = req.URL.String(), string(raw)
			return jsonResponse(200, map[string]any{"accessToken": "aoa-new", "expiresIn": 60}), nil
		})},
	})
	if err != nil || out.AccessToken != "aoa-new" || out.Refresh != "rt-keep" {
		t.Fatalf("out=%#v err=%v", out, err)
	}
	if gotURL != "https://prod.eu-west-1.auth.desktop.kiro.dev/refreshToken" || !strings.Contains(gotBody, `"refreshToken":"rt-keep"`) {
		t.Fatalf("url=%s body=%s", gotURL, gotBody)
	}
}

func TestRefreshOIDCUsesStoredClientNotForeignCLI(t *testing.T) {
	root := t.TempDir()
	writeJSON(t, filepath.Join(root, "other.json"), map[string]any{
		"accessToken":  "aoa-other",
		"refreshToken": "rt-other",
		"region":       "ap-southeast-1",
		"clientId":     "other-client",
		"clientSecret": "other-secret",
	})
	var gotURL string
	var gotBody map[string]any
	_, err := RefreshToken(context.Background(), RefreshInput{
		Refresh: "rt-stored",
		Host:    Host{Platform: runtime.GOOS, Home: root, Env: map[string]string{"KIRO_CREDS_FILE": filepath.Join(root, "other.json")}},
		Stored: &RefreshAccount{
			ProfileARN:   "arn:aws:codewhisperer:eu-west-1:123456789012:profile/stored",
			SSORegion:    "eu-west-1",
			ClientID:     "stored-client",
			ClientSecret: "stored-secret",
		},
		HTTPClient: &http.Client{Transport: refreshRoundTrip(func(req *http.Request) (*http.Response, error) {
			gotURL = req.URL.String()
			_ = json.NewDecoder(req.Body).Decode(&gotBody)
			return jsonResponse(200, map[string]any{"accessToken": "aoa-stored-new", "expiresIn": 60}), nil
		})},
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotURL != "https://oidc.eu-west-1.amazonaws.com/token" {
		t.Fatalf("url=%s", gotURL)
	}
	if gotBody["clientId"] != "stored-client" || gotBody["refreshToken"] != "rt-stored" {
		t.Fatalf("body=%v", gotBody)
	}
}

func TestRefreshLegacyStoredAccountDoesNotBorrowLocalRegion(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "data.sqlite3")
	seedAuthDB(t, dbPath, [][2]string{{
		"kirocli:social:token",
		mustJSON(map[string]any{
			"access_token":  "aoa-other-cli",
			"refresh_token": "rt-other-cli",
			"region":        "ap-southeast-1",
			"profile_arn":   "arn:aws:codewhisperer:ap-southeast-1:123456789012:profile/other",
		}),
	}}, nil)
	var gotURL string
	_, err := RefreshToken(context.Background(), RefreshInput{
		Refresh: "rt-legacy",
		Host:    Host{Platform: runtime.GOOS, Home: root, Env: map[string]string{"KIROCLI_DB_PATH": dbPath, "KIRO_REGION": "eu-central-1"}},
		Stored:  &RefreshAccount{},
		HTTPClient: &http.Client{Transport: refreshRoundTrip(func(req *http.Request) (*http.Response, error) {
			gotURL = req.URL.String()
			return jsonResponse(200, map[string]any{"accessToken": "aoa-legacy-new", "expiresIn": 60}), nil
		})},
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotURL != "https://prod.us-east-1.auth.desktop.kiro.dev/refreshToken" {
		t.Fatalf("url=%s", gotURL)
	}
}

func TestRefreshAccountlessHonorsKIRORegion(t *testing.T) {
	var gotURL string
	_, err := RefreshToken(context.Background(), RefreshInput{
		Refresh: "rt-accountless",
		Host:    Host{Platform: runtime.GOOS, Home: t.TempDir(), Env: map[string]string{"KIRO_REGION": "eu-central-1"}},
		HTTPClient: &http.Client{Transport: refreshRoundTrip(func(req *http.Request) (*http.Response, error) {
			gotURL = req.URL.String()
			return jsonResponse(200, map[string]any{"accessToken": "aoa-env", "expiresIn": 60}), nil
		})},
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotURL != "https://prod.eu-central-1.auth.desktop.kiro.dev/refreshToken" {
		t.Fatalf("url=%s", gotURL)
	}
}

func TestRefreshRetriesRotatedLocalTokenOnlyForSameProfile(t *testing.T) {
	root := t.TempDir()
	profile := "arn:aws:codewhisperer:eu-west-1:123456789012:profile/same"
	dbPath := filepath.Join(root, "data.sqlite3")
	seedAuthDB(t, dbPath, [][2]string{{
		"kirocli:social:token",
		mustJSON(map[string]any{"access_token": "aoa-local-new", "refresh_token": "rt-local-new", "profile_arn": profile, "region": "eu-west-1"}),
	}}, nil)
	var urls []string
	var tokens []string
	out, err := RefreshToken(context.Background(), RefreshInput{
		Refresh: "rt-stored-old",
		Host:    Host{Platform: runtime.GOOS, Home: root, Env: map[string]string{"KIROCLI_DB_PATH": dbPath}},
		Stored: &RefreshAccount{
			ProfileARN:   profile,
			SSORegion:    "eu-west-1",
			ClientID:     "stale-client",
			ClientSecret: "stale-secret",
			Source:       "local-cli",
		},
		HTTPClient: &http.Client{Transport: refreshRoundTrip(func(req *http.Request) (*http.Response, error) {
			var body map[string]any
			_ = json.NewDecoder(req.Body).Decode(&body)
			urls = append(urls, req.URL.String())
			tokens = append(tokens, body["refreshToken"].(string))
			if len(urls) == 1 {
				return jsonResponse(400, map[string]any{"error": "invalid_grant"}), nil
			}
			return jsonResponse(200, map[string]any{"accessToken": "aoa-recovered", "expiresIn": 60}), nil
		})},
	})
	if err != nil || out.AccessToken != "aoa-recovered" || out.Refresh != "rt-local-new" || out.ClientID != "" {
		t.Fatalf("out=%#v err=%v", out, err)
	}
	if len(urls) != 2 || urls[0] != "https://oidc.eu-west-1.amazonaws.com/token" || urls[1] != "https://prod.eu-west-1.auth.desktop.kiro.dev/refreshToken" {
		t.Fatalf("urls=%v tokens=%v", urls, tokens)
	}
}

func TestRefreshDoesNotRetryForeignProfileRotation(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "data.sqlite3")
	seedAuthDB(t, dbPath, [][2]string{{
		"kirocli:social:token",
		mustJSON(map[string]any{
			"access_token":  "aoa-other",
			"refresh_token": "rt-other-new",
			"profile_arn":   "arn:aws:codewhisperer:eu-west-1:123456789012:profile/other",
		}),
	}}, nil)
	calls := 0
	_, err := RefreshToken(context.Background(), RefreshInput{
		Refresh: "rt-stored-old",
		Host:    Host{Platform: runtime.GOOS, Home: root, Env: map[string]string{"KIROCLI_DB_PATH": dbPath}},
		Stored: &RefreshAccount{
			ProfileARN: "arn:aws:codewhisperer:eu-west-1:123456789012:profile/stored",
			SSORegion:  "eu-west-1",
			Source:     "local-cli",
		},
		HTTPClient: &http.Client{Transport: refreshRoundTrip(func(req *http.Request) (*http.Response, error) {
			calls++
			return jsonResponse(400, map[string]any{"error": "invalid_grant"}), nil
		})},
	})
	var refreshErr *RefreshError
	if !errors.As(err, &refreshErr) || refreshErr.Status != 400 || calls != 1 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
	if strings.Contains(err.Error(), "rt-") || strings.Contains(err.Error(), "aoa-") {
		t.Fatalf("leaked=%v", err)
	}
}

func TestRefreshRejectsEmptyTokenAndMissingAccess(t *testing.T) {
	if _, err := RefreshToken(context.Background(), RefreshInput{}); !errors.Is(err, ErrNoRefreshToken) {
		t.Fatalf("empty=%v", err)
	}
	_, err := RefreshToken(context.Background(), RefreshInput{
		Refresh: "rt-old",
		HTTPClient: &http.Client{Transport: refreshRoundTrip(func(req *http.Request) (*http.Response, error) {
			return jsonResponse(200, map[string]any{"refreshToken": "rt-only"}), nil
		})},
	})
	if err == nil || !strings.Contains(err.Error(), "no accessToken") {
		t.Fatalf("missing access=%v", err)
	}
}

func TestRefreshHonorsCallerCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := RefreshToken(ctx, RefreshInput{
		Refresh: "rt-old",
		HTTPClient: &http.Client{Transport: refreshRoundTrip(func(req *http.Request) (*http.Response, error) {
			return jsonResponse(200, map[string]any{"accessToken": "nope"}), nil
		})},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := RefreshToken(ctx, RefreshInput{
			Refresh: "rt-old",
			HTTPClient: &http.Client{Transport: refreshRoundTrip(func(req *http.Request) (*http.Response, error) {
				close(started)
				<-req.Context().Done()
				return nil, req.Context().Err()
			})},
		})
		done <- err
	}()
	<-started
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("in-flight=%v", err)
	}
}

func TestRefreshIgnoresAmbiguousLocalStoreForStoredAccount(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "amb.sqlite3")
	seedAuthDB(t, dbPath, [][2]string{
		{"custom:a:token", mustJSON(map[string]any{"access_token": "aoa-a", "refresh_token": "rt-a"})},
		{"custom:b:token", mustJSON(map[string]any{"access_token": "aoa-b", "refresh_token": "rt-b"})},
	}, nil)
	out, err := RefreshToken(context.Background(), RefreshInput{
		Refresh: "rt-stored",
		Host:    Host{Platform: runtime.GOOS, Home: root, Env: map[string]string{"KIROCLI_DB_PATH": dbPath}},
		Stored: &RefreshAccount{
			SSORegion:    "eu-west-1",
			ClientID:     "stored-client",
			ClientSecret: "stored-secret",
		},
		HTTPClient: &http.Client{Transport: refreshRoundTrip(func(req *http.Request) (*http.Response, error) {
			return jsonResponse(200, map[string]any{"accessToken": "aoa-stored-new", "expiresIn": 60}), nil
		})},
	})
	if err != nil || out.AccessToken != "aoa-stored-new" || out.Refresh != "rt-stored" {
		t.Fatalf("out=%#v err=%v", out, err)
	}
}
