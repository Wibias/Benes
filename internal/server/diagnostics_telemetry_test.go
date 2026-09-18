package server

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/sessions"
	"github.com/Wibias/Benes/internal/timeline"
)

func TestRequestTelemetryRingEvictsToConfiguredMaximum(t *testing.T) {
	state := newRequestTelemetryState()
	for i := 0; i < requestLogMax+25; i++ {
		state.append(requestTelemetryRecord{RequestID: "req_" + strings.Repeat("a", 8) + itoa(i), LegacyID: itoa(i)})
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if len(state.records) != requestLogMax {
		t.Fatalf("len=%d want=%d", len(state.records), requestLogMax)
	}
	if state.records[0].LegacyID != itoa(25) {
		t.Fatalf("oldest=%q", state.records[0].LegacyID)
	}
}

func TestCopyTelemetryAttemptsCapsAndSanitizes(t *testing.T) {
	values := make([]sessions.Attempt, diagnosticsAttemptMax+4)
	for i := range values {
		values[i] = sessions.Attempt{Member: strings.Repeat("m", diagnosticsStringMax+8), Status: 503, Code: "hop-not-safe", Decision: "hop"}
	}
	out := copyTelemetryAttempts(values)
	if len(out) != diagnosticsAttemptMax {
		t.Fatalf("len=%d", len(out))
	}
	if out[0].Ordinal != 1 || out[0].Code != "" || len(out[0].Member) != diagnosticsStringMax {
		t.Fatalf("attempt=%#v", out[0])
	}
}

func TestCopyTelemetryTimelineIgnoresFailedMilestonesForTiming(t *testing.T) {
	failed := timeline.Event{
		Stage:     timeline.StageUpstreamWaitHeaders,
		Side:      timeline.SideUpstream,
		Milestone: timeline.MilestoneHeaders,
		OK:        false,
		Cause:     "headers",
		Elapsed:   12 * time.Millisecond,
	}
	out, timing, failure, code := copyTelemetryTimeline([]timeline.Event{failed})
	if len(out) != 1 || out[0].OK || out[0].Milestone != string(timeline.MilestoneHeaders) {
		t.Fatalf("timeline=%#v", out)
	}
	if timing.HeadersMs != nil {
		t.Fatalf("failed headers populated headersMs=%v", *timing.HeadersMs)
	}
	if failure == nil || failure.Cause != "headers" || failure.Stage != string(timeline.StageUpstreamWaitHeaders) || code != "headers" {
		t.Fatalf("failure=%#v code=%q", failure, code)
	}

	laterOK := timeline.Event{
		Stage:     timeline.StageUpstreamWaitHeaders,
		Side:      timeline.SideUpstream,
		Milestone: timeline.MilestoneHeaders,
		OK:        true,
		Elapsed:   18 * time.Millisecond,
	}
	_, timing, failure, _ = copyTelemetryTimeline([]timeline.Event{failed, laterOK})
	if timing.HeadersMs == nil || *timing.HeadersMs != 18 {
		t.Fatalf("successful headersMs=%v", timing.HeadersMs)
	}
	if failure == nil || failure.Cause != "headers" {
		t.Fatalf("failure dropped=%#v", failure)
	}

	milestones := []struct {
		name      timeline.Milestone
		got       func(diagnosticsTiming) *int64
		failStage timeline.Stage
		okStage   timeline.Stage
	}{
		{timeline.MilestoneHeaders, func(t diagnosticsTiming) *int64 { return t.HeadersMs }, timeline.StageUpstreamWaitHeaders, timeline.StageUpstreamWaitHeaders},
		{timeline.MilestoneFirstByte, func(t diagnosticsTiming) *int64 { return t.FirstByteMs }, timeline.StageUpstreamRead, timeline.StageUpstreamRead},
		{timeline.MilestoneTTFT, func(t diagnosticsTiming) *int64 { return t.TTFTMs }, timeline.StageUpstreamRead, timeline.StageUpstreamRead},
		{timeline.MilestoneFirstDownstream, func(t diagnosticsTiming) *int64 { return t.FirstDownstreamMs }, timeline.StageDownstreamWrite, timeline.StageDownstreamWrite},
		{timeline.MilestoneUpstreamEnd, func(t diagnosticsTiming) *int64 { return t.UpstreamEndMs }, timeline.StageUpstreamRead, timeline.StageUpstreamRead},
		{timeline.MilestoneDownstreamEnd, func(t diagnosticsTiming) *int64 { return t.DownstreamEndMs }, timeline.StageDownstreamWrite, timeline.StageDownstreamWrite},
	}
	for _, item := range milestones {
		fail := timeline.Event{Stage: item.failStage, Side: timeline.SideUpstream, Milestone: item.name, OK: false, Cause: "connection_reset", Elapsed: 5 * time.Millisecond}
		ok := timeline.Event{Stage: item.okStage, Side: timeline.SideUpstream, Milestone: item.name, OK: true, Elapsed: 9 * time.Millisecond}
		_, onlyFail, failAttr, _ := copyTelemetryTimeline([]timeline.Event{fail})
		if item.got(onlyFail) != nil {
			t.Fatalf("%s failed milestone populated timing", item.name)
		}
		if failAttr == nil || failAttr.Cause != "connection_reset" {
			t.Fatalf("%s failure=%#v", item.name, failAttr)
		}
		_, both, _, _ := copyTelemetryTimeline([]timeline.Event{fail, ok})
		got := item.got(both)
		if got == nil || *got != 9 {
			t.Fatalf("%s successful timing=%v", item.name, got)
		}
	}
}

func TestClipTelemetryStringIsUTF8SafeAndBounded(t *testing.T) {
	if got := clipTelemetryString("client-corr-1", diagnosticsStringMax); got != "client-corr-1" {
		t.Fatalf("normal=%q", got)
	}
	oversized := strings.Repeat("x", diagnosticsStringMax+64)
	if got := clipTelemetryString(oversized, diagnosticsStringMax); len(got) != diagnosticsStringMax || strings.ContainsRune(got, utf8.RuneError) {
		t.Fatalf("ascii clip len=%d", len(got))
	}
	split := strings.Repeat("a", diagnosticsStringMax-1) + "é"
	got := clipTelemetryString(split, diagnosticsStringMax)
	if !utf8.ValidString(got) || strings.ContainsRune(got, utf8.RuneError) {
		t.Fatalf("invalid utf8 %q", got)
	}
	if strings.Contains(got, "é") {
		t.Fatalf("split rune kept %q", got)
	}
	if len(got) != diagnosticsStringMax-1 {
		t.Fatalf("split len=%d", len(got))
	}
}

func TestCopyTelemetryTimelineCapsAndDropsUnsafeCauses(t *testing.T) {
	events := make([]timeline.Event, diagnosticsTimelineEventMax+3)
	for i := range events {
		events[i] = timeline.Event{Stage: timeline.StagePreDispatch, Side: timeline.SideLocal, OK: true, Cause: `{"requested_gateway_routing":"secret"}`}
	}
	events[1] = timeline.Event{Stage: timeline.StageUpstreamWaitHeaders, Side: timeline.SideUpstream, Milestone: timeline.MilestoneFirstByte, OK: true}
	events[2] = timeline.Event{Stage: timeline.StageUpstreamRead, Side: timeline.SideUpstream, Milestone: timeline.MilestoneTTFT, OK: true}
	events[3] = timeline.Event{Stage: timeline.StageUpstreamWaitHeaders, Side: timeline.SideUpstream, OK: false, Cause: "connection_reset"}
	out, timing, failure, code := copyTelemetryTimeline(events)
	if len(out) != diagnosticsTimelineEventMax {
		t.Fatalf("len=%d", len(out))
	}
	if out[0].NormalizedCause != "" {
		t.Fatalf("leaked cause %q", out[0].NormalizedCause)
	}
	if timing.FirstByteMs == nil || timing.TTFTMs == nil {
		t.Fatalf("timing=%#v", timing)
	}
	if failure == nil || failure.Cause != "connection_reset" || code != "connection_reset" {
		t.Fatalf("failure=%#v code=%q", failure, code)
	}
}

func TestSanitizeNormalizedCauseRejectsPayloads(t *testing.T) {
	if got := sanitizeNormalizedCause("admission_denied"); got != "admission_denied" {
		t.Fatalf("got=%q", got)
	}
	if got := sanitizeNormalizedCause(`{"apiKey":"sk-secret"}`); got != "" {
		t.Fatalf("payload leaked %q", got)
	}
	if got := sanitizeNormalizedCause("Authorization Bearer abc"); got != "" {
		t.Fatalf("header leaked %q", got)
	}
}

func TestProjectDiagnosticsUsageKeepsProvenance(t *testing.T) {
	if projectDiagnosticsUsage(nil) != nil {
		t.Fatal("nil usage should stay absent")
	}
	reported := projectDiagnosticsUsage(&protocol.Usage{InputTokens: 4, OutputTokens: 2})
	if reported.Status != "reported" || *reported.TotalTokens != 6 {
		t.Fatalf("reported=%#v", reported)
	}
	estimated := projectDiagnosticsUsage(&protocol.Usage{InputTokens: 4, OutputTokens: 2, Estimated: true})
	if estimated.Status != "estimated" {
		t.Fatalf("estimated=%#v", estimated)
	}
}

func TestDecodeDiagnosticsCursorRejectsMalformed(t *testing.T) {
	if _, _, err := decodeDiagnosticsCursor(""); err == nil {
		t.Fatal("empty")
	}
	if _, _, err := decodeDiagnosticsCursor("%%%"); err == nil {
		t.Fatal("junk")
	}
	if _, _, err := decodeDiagnosticsCursor(encodeDiagnosticsCursor("abc", 3) + "x"); err == nil {
		t.Fatal("suffix")
	}
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [16]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
