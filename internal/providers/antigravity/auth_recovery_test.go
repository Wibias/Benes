package antigravity

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/providers"
)

type scriptedAccountAuthority struct {
	mu        sync.Mutex
	current   map[string]Account
	refreshes atomic.Int32
	refresh   func(context.Context, Account) (Account, error)
}

func (a *scriptedAccountAuthority) Snapshot(ctx context.Context, accountID string) (Account, error) {
	if err := ctx.Err(); err != nil {
		return Account{}, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	account, ok := a.current[accountID]
	if !ok {
		return Account{}, ErrAccountAuthorityUnavailable
	}
	return account, nil
}

func (a *scriptedAccountAuthority) RefreshAfterUnauthorized(ctx context.Context, stale Account) (Account, error) {
	a.refreshes.Add(1)
	if a.refresh != nil {
		account, err := a.refresh(ctx, stale)
		if err != nil {
			return Account{}, err
		}
		a.mu.Lock()
		a.current[account.ID] = account
		a.mu.Unlock()
		return account, nil
	}
	return Account{}, ErrAccountAuthorityUnavailable
}

func TestClient401ReplaysSameAccountWithRefreshedSnapshot(t *testing.T) {
	var hits int32
	var seen []string

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&hits, 1)
		raw, _ := io.ReadAll(r.Body)
		seen = append(seen, r.Header.Get("Authorization")+" "+string(raw))
		if n == 1 {
			if r.Header.Get("Authorization") != "Bearer stale-a" || !strings.Contains(string(raw), `"project":"project-a-old"`) {
				t.Errorf("first attempt did not use account A stale snapshot: auth=%q body=%s", r.Header.Get("Authorization"), raw)
			}
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.Header.Get("Authorization") != "Bearer fresh-a" || !strings.Contains(string(raw), `"project":"project-a-new"`) {
			t.Errorf("replay did not rebind account A refreshed snapshot: auth=%q body=%s", r.Header.Get("Authorization"), raw)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, ccaOK)
	}))
	defer upstream.Close()

	authority := &scriptedAccountAuthority{
		current: map[string]Account{
			"a": {ID: "a", Token: "stale-a", ProjectID: "project-a-old"},
			"b": {ID: "b", Token: "token-b", ProjectID: "project-b"},
		},
		refresh: func(ctx context.Context, stale Account) (Account, error) {
			if stale.ID != "a" || stale.Token != "stale-a" || stale.ProjectID != "project-a-old" {
				t.Fatalf("unexpected stale snapshot: %#v", stale)
			}
			return Account{ID: "a", Token: "fresh-a", ProjectID: "project-a-new"}, nil
		},
	}
	client := newTestClient(t, upstream, []Account{
		{ID: "a", Token: "stale-a", ProjectID: "project-a-old"},
		{ID: "b", Token: "token-b", ProjectID: "project-b"},
	}, false)
	client.authority = authority

	stream, err := client.Open(context.Background(), providers.DispatchRequest{
		Parsed: protocol.ParsedRequest{UpstreamModelID: "gemini-3.7-flash"},
	})
	if err != nil {
		t.Fatalf("recoverable same-account 401 terminated request: %v", err)
	}
	defer stream.Close()

	event, err := stream.Next()
	if err != nil || event.Text != "ok" {
		t.Fatalf("event=%#v err=%v", event, err)
	}
	if atomic.LoadInt32(&hits) != 2 {
		t.Fatalf("provider attempts=%d, want exactly 2", hits)
	}
	if authority.refreshes.Load() != 1 {
		t.Fatalf("refreshes=%d, want 1", authority.refreshes.Load())
	}
	for _, attempt := range seen {
		if strings.Contains(attempt, "Bearer token-b") || strings.Contains(attempt, `"project":"project-b"`) {
			t.Fatalf("401 recovery rotated to account B: %v", seen)
		}
	}
}

func TestClient401ReplayIsBoundedToOneRefresh(t *testing.T) {
	var hits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer upstream.Close()

	authority := &scriptedAccountAuthority{
		current: map[string]Account{"a": {ID: "a", Token: "stale", ProjectID: "old-project"}},
		refresh: func(context.Context, Account) (Account, error) {
			return Account{ID: "a", Token: "fresh", ProjectID: "new-project"}, nil
		},
	}
	client := newTestClient(t, upstream, []Account{{ID: "a", Token: "stale", ProjectID: "old-project"}}, false)
	client.authority = authority

	_, err := client.Open(context.Background(), providers.DispatchRequest{Parsed: protocol.ParsedRequest{UpstreamModelID: "gemini-3.7-flash"}})
	if err == nil || !strings.Contains(err.Error(), "HTTP 401") {
		t.Fatalf("err=%v", err)
	}
	if hits.Load() != 2 || authority.refreshes.Load() != 1 {
		t.Fatalf("hits=%d refreshes=%d", hits.Load(), authority.refreshes.Load())
	}
}

func TestClient401RefreshFailureFailsOverToEligibleAccount(t *testing.T) {
	var seen []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		seen = append(seen, auth)
		if auth == "Bearer stale-a" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if auth != "Bearer token-b" {
			t.Errorf("unexpected authorization: %q", auth)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, ccaOK)
	}))
	defer upstream.Close()

	authority := &scriptedAccountAuthority{
		current: map[string]Account{
			"a": {ID: "a", Token: "stale-a", ProjectID: "project-a"},
			"b": {ID: "b", Token: "token-b", ProjectID: "project-b"},
		},
		refresh: func(context.Context, Account) (Account, error) {
			return Account{}, errors.New("refresh failed")
		},
	}
	client := newTestClient(t, upstream, []Account{
		{ID: "a", Token: "stale-a", ProjectID: "project-a"},
		{ID: "b", Token: "token-b", ProjectID: "project-b"},
	}, false)
	client.authority = authority

	stream, err := client.Open(context.Background(), providers.DispatchRequest{Parsed: protocol.ParsedRequest{UpstreamModelID: "gemini-3.7-flash"}})
	if err != nil {
		t.Fatalf("refresh failure should use another eligible account before output: %v", err)
	}
	defer stream.Close()
	event, err := stream.Next()
	if err != nil || event.Text != "ok" {
		t.Fatalf("event=%#v err=%v", event, err)
	}
	if len(seen) != 2 || seen[0] != "Bearer stale-a" || seen[1] != "Bearer token-b" {
		t.Fatalf("account recovery=%v", seen)
	}
}

func TestClient401RotatesAfterRefreshedAccountStillUnauthorized(t *testing.T) {
	var seen []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		seen = append(seen, auth)
		switch auth {
		case "Bearer stale-a", "Bearer fresh-a":
			http.Error(w, "unauthorized", http.StatusUnauthorized)
		case "Bearer token-b":
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, ccaOK)
		default:
			t.Errorf("unexpected authorization: %q", auth)
		}
	}))
	defer upstream.Close()

	authority := &scriptedAccountAuthority{
		current: map[string]Account{
			"a": {ID: "a", Token: "stale-a", ProjectID: "project-a-old"},
			"b": {ID: "b", Token: "token-b", ProjectID: "project-b"},
		},
		refresh: func(context.Context, Account) (Account, error) {
			return Account{ID: "a", Token: "fresh-a", ProjectID: "project-a-new"}, nil
		},
	}
	client := newTestClient(t, upstream, []Account{
		{ID: "a", Token: "stale-a", ProjectID: "project-a-old"},
		{ID: "b", Token: "token-b", ProjectID: "project-b"},
	}, false)
	client.authority = authority

	stream, err := client.Open(context.Background(), providers.DispatchRequest{Parsed: protocol.ParsedRequest{UpstreamModelID: "gemini-3.7-flash"}})
	if err != nil {
		t.Fatalf("second 401 should fail over after one same-account refresh: %v", err)
	}
	defer stream.Close()
	event, err := stream.Next()
	if err != nil || event.Text != "ok" {
		t.Fatalf("event=%#v err=%v", event, err)
	}
	if authority.refreshes.Load() != 1 {
		t.Fatalf("refreshes=%d, want 1", authority.refreshes.Load())
	}
	want := []string{"Bearer stale-a", "Bearer fresh-a", "Bearer token-b"}
	if len(seen) != len(want) {
		t.Fatalf("authorization sequence=%v", seen)
	}
	for i := range want {
		if seen[i] != want[i] {
			t.Fatalf("authorization sequence=%v", seen)
		}
	}
}

func TestClient401GivesEachFailedOverAccountOneRefresh(t *testing.T) {
	var seen []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		seen = append(seen, auth)
		switch auth {
		case "Bearer stale-a", "Bearer fresh-a", "Bearer stale-b":
			http.Error(w, "unauthorized", http.StatusUnauthorized)
		case "Bearer fresh-b":
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, ccaOK)
		default:
			t.Errorf("unexpected authorization: %q", auth)
		}
	}))
	defer upstream.Close()

	authority := &scriptedAccountAuthority{
		current: map[string]Account{
			"a": {ID: "a", Token: "stale-a", ProjectID: "project-a-old"},
			"b": {ID: "b", Token: "stale-b", ProjectID: "project-b-old"},
		},
		refresh: func(_ context.Context, stale Account) (Account, error) {
			switch stale.ID {
			case "a":
				return Account{ID: "a", Token: "fresh-a", ProjectID: "project-a-new"}, nil
			case "b":
				return Account{ID: "b", Token: "fresh-b", ProjectID: "project-b-new"}, nil
			default:
				return Account{}, errors.New("unexpected account")
			}
		},
	}
	client := newTestClient(t, upstream, []Account{
		{ID: "a", Token: "stale-a", ProjectID: "project-a-old"},
		{ID: "b", Token: "stale-b", ProjectID: "project-b-old"},
	}, false)
	client.authority = authority

	stream, err := client.Open(context.Background(), providers.DispatchRequest{
		Parsed: protocol.ParsedRequest{UpstreamModelID: "gemini-3.7-flash"},
	})
	if err != nil {
		t.Fatalf("second account should retain its own one-refresh recovery: %v", err)
	}
	defer stream.Close()
	event, err := stream.Next()
	if err != nil || event.Text != "ok" {
		t.Fatalf("event=%#v err=%v", event, err)
	}
	if authority.refreshes.Load() != 2 {
		t.Fatalf("refreshes=%d, want 2", authority.refreshes.Load())
	}
	want := []string{"Bearer stale-a", "Bearer fresh-a", "Bearer stale-b", "Bearer fresh-b"}
	if len(seen) != len(want) {
		t.Fatalf("authorization sequence=%v", seen)
	}
	for i := range want {
		if seen[i] != want[i] {
			t.Fatalf("authorization sequence=%v", seen)
		}
	}
}

func TestClient401RejectsRefreshedSnapshotFromDifferentAccount(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer upstream.Close()
	authority := &scriptedAccountAuthority{
		current: map[string]Account{"a": {ID: "a", Token: "stale-a", ProjectID: "project-a"}},
		refresh: func(context.Context, Account) (Account, error) {
			return Account{ID: "b", Token: "fresh-b", ProjectID: "project-b"}, nil
		},
	}
	client := newTestClient(t, upstream, []Account{{ID: "a", Token: "stale-a", ProjectID: "project-a"}}, false)
	client.authority = authority
	_, err := client.Open(context.Background(), providers.DispatchRequest{Parsed: protocol.ParsedRequest{UpstreamModelID: "gemini-3.7-flash"}})
	if err == nil || err.Error() != "Cloud Code Assist authentication recovery failed" {
		t.Fatalf("err=%v", err)
	}
}

func TestClient401RefreshCancellationIsAuthoritative(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer upstream.Close()

	refreshStarted := make(chan struct{})
	authority := &scriptedAccountAuthority{
		current: map[string]Account{"a": {ID: "a", Token: "stale-a", ProjectID: "project-a"}},
		refresh: func(ctx context.Context, _ Account) (Account, error) {
			close(refreshStarted)
			<-ctx.Done()
			return Account{}, ctx.Err()
		},
	}
	client := newTestClient(t, upstream, []Account{{ID: "a", Token: "stale-a", ProjectID: "project-a"}}, false)
	client.authority = authority

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		_, err := client.Open(ctx, providers.DispatchRequest{Parsed: protocol.ParsedRequest{UpstreamModelID: "gemini-3.7-flash"}})
		errCh <- err
	}()
	<-refreshStarted
	cancel()
	if err := <-errCh; !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
	if authority.refreshes.Load() != 1 {
		t.Fatalf("refreshes=%d, want 1", authority.refreshes.Load())
	}
}
