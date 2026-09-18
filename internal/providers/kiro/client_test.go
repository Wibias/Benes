package kiro

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wibias/Benes/internal/credentialpool"
	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/providers"
)

func TestNewHardenedRejectsNonAWSShapedRegion(t *testing.T) {
	_, err := NewHardened(context.Background(), Config{
		Account: AccountSnapshot{
			AccessToken: "tok",
			APIRegion:   "evil",
			AuthType:    AuthBuilderID,
		},
		HTTPClient: &http.Client{},
	})
	if err == nil {
		t.Fatal("accepted evil region")
	}
	_, err = NewHardened(context.Background(), Config{
		Account: AccountSnapshot{
			AccessToken: "tok",
			ProfileARN:  "arn:aws:codewhisperer:evil:123456789012:profile/abc",
			AuthType:    AuthBuilderID,
		},
		HTTPClient: &http.Client{},
	})
	if err == nil {
		t.Fatal("accepted evil profile-ARN region")
	}
}

func TestOpenPostsGenerateAssistantResponseAndReadsEventStream(t *testing.T) {
	var gotTarget, gotAuth, gotProfile string
	frame := EncodeEventStreamMessage(map[string]string{":event-type": "assistantResponseEvent"}, []byte(`{"content":"hello"}`))
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTarget = r.Header.Get("x-amz-target")
		gotAuth = r.Header.Get("Authorization")
		gotProfile = r.Header.Get("x-amzn-kiro-profile-arn")
		w.Header().Set("Content-Type", "application/vnd.amazon.eventstream")
		_, _ = w.Write(frame)
	}))
	t.Cleanup(upstream.Close)
	client, err := NewHardened(context.Background(), Config{
		Account: AccountSnapshot{
			AccessToken: "tok",
			ProfileARN:  "arn:aws:codewhisperer:us-east-1:123456789012:profile/abc",
			APIRegion:   "us-east-1",
		},
		HTTPClient: upstream.Client(),
		Endpoint:   "https://runtime.us-east-1.kiro.dev",
	})
	if err != nil {
		t.Fatal(err)
	}
	// Rewrite to httptest while keeping the validated HTTPS host in Config.Endpoint.
	client.endpoint = upstream.URL
	stream, err := client.Open(context.Background(), providers.DispatchRequest{Parsed: protocol.ParsedRequest{
		UpstreamModelID: "claude-sonnet-4",
		Context: protocol.Context{Messages: []protocol.Message{{
			Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "hi"}},
		}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	ev, err := stream.Next()
	if err != nil && err != io.EOF {
		t.Fatal(err)
	}
	if ev.Text != "hello" {
		t.Fatalf("event=%#v", ev)
	}
	if gotTarget != generateTarget || gotAuth != "Bearer tok" || gotProfile == "" {
		t.Fatalf("target=%q auth=%q profile=%q", gotTarget, gotAuth, gotProfile)
	}
}

func TestOpenFailsOver429FromExhaustedAccountToHeadroom(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	a := kiroPoolAccount("a", "tok-a")
	b := kiroPoolAccount("b", "tok-b")
	auths, profiles, err := openKiroPool(t, Config{
		Account:  a,
		Accounts: []AccountSnapshot{a, b},
		Evidence: map[string]credentialpool.Evidence{
			a.ProfileARN: kiroKnownEvidence(now, 1, false),
			b.ProfileARN: kiroKnownEvidence(now, 0.2, false),
		},
		Now: now,
	}, map[string]int{"Bearer tok-a": http.StatusTooManyRequests})
	if err != nil {
		t.Fatalf("failover err=%v auths=%q", err, auths)
	}
	if countAuth(auths, "Bearer tok-a") != 1 || auths[len(auths)-1] != "Bearer tok-b" {
		t.Fatalf("did not rotate exhausted A to B: %q", auths)
	}
	if profiles[len(profiles)-1] != b.ProfileARN {
		t.Fatalf("did not rebuild profileArn for B: %q", profiles)
	}
}

func TestOpenRetriesSameSnapshotThreeTimesWithoutSecondAccount(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	a := kiroPoolAccount("a", "tok-a")
	auths, _, err := openKiroPool(t, Config{Account: a, Now: now}, map[string]int{"Bearer tok-a": http.StatusTooManyRequests})
	if err == nil {
		t.Fatal("expected 429 after same-account retries")
	}
	if countAuth(auths, "Bearer tok-a") != maxTransientAttempts {
		t.Fatalf("same-account retries=%q", auths)
	}
}

func TestOpenFailsOver429FromExhaustedAccountToUnknown(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	a := kiroPoolAccount("a", "tok-a")
	b := kiroPoolAccount("b", "tok-b")
	auths, _, err := openKiroPool(t, Config{
		Account:  a,
		Accounts: []AccountSnapshot{a, b},
		Evidence: map[string]credentialpool.Evidence{
			a.ProfileARN: kiroKnownEvidence(now, 1, false),
		},
		Now: now,
	}, map[string]int{"Bearer tok-a": http.StatusTooManyRequests})
	if err != nil {
		t.Fatalf("unknown fallback err=%v auths=%q", err, auths)
	}
	if countAuth(auths, "Bearer tok-a") != 1 || auths[len(auths)-1] != "Bearer tok-b" {
		t.Fatalf("did not fall back from exhausted A to unknown B: %q", auths)
	}
}

func TestOpenFailsOver429TowardKnownHeadroomOverUnknown(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	a := kiroPoolAccount("a", "tok-a")
	b := kiroPoolAccount("b", "tok-b")
	c := kiroPoolAccount("c", "tok-c")
	auths, _, err := openKiroPool(t, Config{
		Account:  a,
		Accounts: []AccountSnapshot{a, b, c},
		Evidence: map[string]credentialpool.Evidence{
			a.ProfileARN: kiroKnownEvidence(now, 1, false),
			c.ProfileARN: kiroKnownEvidence(now, 0.1, false),
		},
		Now: now,
	}, map[string]int{"Bearer tok-a": http.StatusTooManyRequests})
	if err != nil {
		t.Fatalf("headroom rank err=%v auths=%q", err, auths)
	}
	if auths[len(auths)-1] != "Bearer tok-c" {
		t.Fatalf("unknown B beat known headroom C: %q", auths)
	}
}

func TestOpenFailsOver429ToOverageEnabledAccountAtCap(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	a := kiroPoolAccount("a", "tok-a")
	b := kiroPoolAccount("b", "tok-b")
	auths, _, err := openKiroPool(t, Config{
		Account:  a,
		Accounts: []AccountSnapshot{a, b},
		Evidence: map[string]credentialpool.Evidence{
			a.ProfileARN: kiroKnownEvidence(now, 1, false),
			b.ProfileARN: kiroKnownEvidence(now, 1, true),
		},
		Now: now,
	}, map[string]int{"Bearer tok-a": http.StatusTooManyRequests})
	if err != nil {
		t.Fatalf("overage failover err=%v auths=%q", err, auths)
	}
	if auths[len(auths)-1] != "Bearer tok-b" {
		t.Fatalf("overage-at-cap B was treated as exhausted: %q", auths)
	}
}

func TestOpenDoesNotRotateCrossRegionAccountOn429(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	a := kiroPoolAccount("a", "tok-a")
	b := AccountSnapshot{AccessToken: "tok-b", ProfileARN: "arn:aws:codewhisperer:eu-west-1:123456789012:profile/b", APIRegion: "eu-west-1", AuthType: AuthKiroDesktop}
	auths, _, err := openKiroPool(t, Config{
		Account:  a,
		Accounts: []AccountSnapshot{a, b},
		Evidence: map[string]credentialpool.Evidence{
			a.ProfileARN: kiroKnownEvidence(now, 1, false),
			b.ProfileARN: kiroKnownEvidence(now, 0.1, false),
		},
		Now: now,
	}, map[string]int{"Bearer tok-a": http.StatusTooManyRequests})
	if err == nil {
		t.Fatal("cross-region B was used as a same-destination failover")
	}
	if countAuth(auths, "Bearer tok-a") != maxTransientAttempts || countAuth(auths, "Bearer tok-b") != 0 {
		t.Fatalf("cross-region leaked into failover: %q", auths)
	}
}

func kiroPoolAccount(id, token string) AccountSnapshot {
	return AccountSnapshot{
		AccessToken: token,
		ProfileARN:  "arn:aws:codewhisperer:us-east-1:123456789012:profile/" + id,
		APIRegion:   "us-east-1",
		AuthType:    AuthKiroDesktop,
	}
}

func kiroKnownEvidence(now time.Time, utilization float64, overage bool) credentialpool.Evidence {
	return credentialpool.Evidence{
		Auth:           credentialpool.AuthUsable,
		Quota:          credentialpool.QuotaKnown,
		Utilization:    utilization,
		ValidUntil:     now.Add(time.Minute),
		Limit:          credentialpool.LimitAvailable,
		ObservedAt:     now,
		OverageEnabled: overage,
	}
}

func openKiroPool(t *testing.T, config Config, fail map[string]int) (auths []string, profiles []string, err error) {
	t.Helper()
	frame := EncodeEventStreamMessage(map[string]string{":event-type": "assistantResponseEvent"}, []byte(`{"content":"ok"}`))
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		auths = append(auths, auth)
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if profile, _ := body["profileArn"].(string); profile != "" {
			profiles = append(profiles, profile)
		}
		if status := fail[auth]; status != 0 {
			w.WriteHeader(status)
			return
		}
		w.Header().Set("Content-Type", "application/vnd.amazon.eventstream")
		_, _ = w.Write(frame)
	}))
	t.Cleanup(upstream.Close)
	config.HTTPClient = upstream.Client()
	config.Endpoint = "https://runtime.us-east-1.kiro.dev"
	client, err := NewHardened(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	client.endpoint = upstream.URL
	stream, err := client.Open(context.Background(), providers.DispatchRequest{Parsed: protocol.ParsedRequest{
		UpstreamModelID: "claude-sonnet-4",
		Context: protocol.Context{Messages: []protocol.Message{{
			Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "hi"}},
		}}},
	}})
	if err != nil {
		return auths, profiles, err
	}
	t.Cleanup(func() { _ = stream.Close() })
	return auths, profiles, nil
}

func countAuth(auths []string, want string) int {
	n := 0
	for _, auth := range auths {
		if auth == want {
			n++
		}
	}
	return n
}

func TestOpenOmitsProfileHeaderForBuilderIDWithoutProfileARN(t *testing.T) {
	var gotProfile string
	var sawProfileHeader bool
	var body map[string]any
	frame := EncodeEventStreamMessage(map[string]string{":event-type": "assistantResponseEvent"}, []byte(`{"content":"ok"}`))
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotProfile = r.Header.Get("x-amzn-kiro-profile-arn")
		_, sawProfileHeader = r.Header["X-Amzn-Kiro-Profile-Arn"]
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/vnd.amazon.eventstream")
		_, _ = w.Write(frame)
	}))
	t.Cleanup(upstream.Close)
	client, err := NewHardened(context.Background(), Config{
		Account: AccountSnapshot{
			AccessToken: "tok",
			APIRegion:   "us-east-1",
			AuthType:    "aws_sso_oidc",
		},
		HTTPClient: upstream.Client(),
		Endpoint:   "https://runtime.us-east-1.kiro.dev",
	})
	if err != nil {
		t.Fatal(err)
	}
	client.endpoint = upstream.URL
	stream, err := client.Open(context.Background(), providers.DispatchRequest{Parsed: protocol.ParsedRequest{
		UpstreamModelID: "claude-sonnet-4",
		Context: protocol.Context{Messages: []protocol.Message{{
			Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "hi"}},
		}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if _, err := stream.Next(); err != nil && err != io.EOF {
		t.Fatal(err)
	}
	if sawProfileHeader || gotProfile != "" {
		t.Fatalf("profile header=%v value=%q", sawProfileHeader, gotProfile)
	}
	if _, ok := body["profileArn"]; ok {
		t.Fatalf("payload synthesized profileArn: %#v", body)
	}
}

func TestOpenAcceptsParallelToolCallsHintWithoutEmittingControl(t *testing.T) {
	var body map[string]any
	frame := EncodeEventStreamMessage(map[string]string{":event-type": "assistantResponseEvent"}, []byte(`{"content":"ok"}`))
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/vnd.amazon.eventstream")
		_, _ = w.Write(frame)
	}))
	t.Cleanup(upstream.Close)
	client, err := NewHardened(context.Background(), Config{
		Account: AccountSnapshot{
			AccessToken: "tok",
			ProfileARN:  "arn:aws:codewhisperer:us-east-1:123456789012:profile/abc",
			APIRegion:   "us-east-1",
		},
		HTTPClient: upstream.Client(),
		Endpoint:   "https://runtime.us-east-1.kiro.dev",
	})
	if err != nil {
		t.Fatal(err)
	}
	client.endpoint = upstream.URL
	yes := true
	stream, err := client.Open(context.Background(), providers.DispatchRequest{Parsed: protocol.ParsedRequest{
		UpstreamModelID: "claude-sonnet-4",
		Options:         protocol.RequestOptions{ParallelToolCalls: &yes},
		Context: protocol.Context{Messages: []protocol.Message{{
			Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "hi"}},
		}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if _, err := stream.Next(); err != nil && err != io.EOF {
		t.Fatal(err)
	}
	for _, key := range []string{"parallel_tool_calls", "parallelToolCalls", "disable_parallel_tool_use"} {
		if jsonContainsKey(body, key) {
			t.Fatalf("emitted unsupported parallel control %q: %#v", key, body)
		}
	}
}

func jsonContainsKey(v any, want string) bool {
	switch t := v.(type) {
	case map[string]any:
		for k, child := range t {
			if k == want || jsonContainsKey(child, want) {
				return true
			}
		}
	case []any:
		for _, child := range t {
			if jsonContainsKey(child, want) {
				return true
			}
		}
	}
	return false
}

func TestIncompleteToolCallOnEOFFailsClosed(t *testing.T) {
	start := EncodeEventStreamMessage(map[string]string{":event-type": "toolUseEvent"}, []byte(`{"name":"lookup","toolUseId":"c1"}`))
	s := &stream{buf: start, body: io.NopCloser(bytes.NewReader(nil)), open: map[string]struct{}{}}
	if _, err := s.Next(); err != nil {
		t.Fatal(err)
	}
	ev, err := s.Next()
	if err != nil || ev.Type != protocol.EventError {
		t.Fatalf("eof incomplete=%#v err=%v", ev, err)
	}
}

func TestStreamDecodesToolUseEvents(t *testing.T) {
	start := EncodeEventStreamMessage(map[string]string{":event-type": "toolUseEvent"}, []byte(`{"name":"lookup","toolUseId":"c1"}`))
	delta := EncodeEventStreamMessage(map[string]string{":event-type": "toolUseEvent"}, []byte(`{"name":"lookup","toolUseId":"c1","input":"{\"q\":"}`))
	end := EncodeEventStreamMessage(map[string]string{":event-type": "toolUseEvent"}, []byte(`{"name":"lookup","toolUseId":"c1","input":"{\"q\":\"x\"}","stop":true}`))
	s := &stream{buf: append(append(start, delta...), end...)}
	ev, err := s.Next()
	if err != nil || ev.Type != protocol.EventToolCallStart || ev.ID != "c1" || ev.Name != "lookup" {
		t.Fatalf("start=%#v err=%v", ev, err)
	}
	ev, err = s.Next()
	if err != nil || ev.Type != protocol.EventToolCallDelta || ev.Arguments != `{"q":` {
		t.Fatalf("delta=%#v err=%v", ev, err)
	}
	ev, err = s.Next()
	if err != nil || ev.Type != protocol.EventToolCallEnd || ev.Arguments != `{"q":"x"}` {
		t.Fatalf("end=%#v err=%v", ev, err)
	}
}

func TestStreamDecodesReasoningAndProviderUsage(t *testing.T) {
	think := EncodeEventStreamMessage(map[string]string{":event-type": "reasoningContentEvent"}, []byte(`{"content":"plan"}`))
	meta := EncodeEventStreamMessage(map[string]string{":event-type": "metadataEvent"}, []byte(`{"usage":{"inputTokens":11,"outputTokens":3}}`))
	s := &stream{buf: append(think, meta...)}
	ev, err := s.Next()
	if err != nil || ev.Type != protocol.EventThinkingDelta || ev.Thinking != "plan" {
		t.Fatalf("think=%#v err=%v", ev, err)
	}
	done, err := s.Next()
	if err != nil || done.Usage == nil || done.Usage.Estimated || done.Usage.InputTokens != 11 || done.Usage.TotalTokens != 14 {
		t.Fatalf("usage=%#v err=%v", done, err)
	}
}
