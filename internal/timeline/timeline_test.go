package timeline

import (
	"testing"
	"time"
)

func TestClassifyNoDispatchIsLocalPreDispatch(t *testing.T) {
	tr := New("req-1", 16)
	tr.Mark(StagePreDispatch, SideLocal, "", false, "compile_error")
	got := tr.Classify()
	if got.Side != SideLocal || got.Stage != StagePreDispatch {
		t.Fatalf("got=%#v", got)
	}
}

func TestClassifyDispatchedWithoutHeadersIsUpstreamWait(t *testing.T) {
	tr := New("req-1", 16)
	tr.Mark(StagePreDispatch, SideLocal, MilestoneDispatch, true, "")
	tr.Mark(StageUpstreamWaitHeaders, SideUpstream, "", false, "timeout")
	got := tr.Classify()
	if got.Side != SideUpstream || got.Stage != StageUpstreamWaitHeaders {
		t.Fatalf("got=%#v", got)
	}
}

func TestClassifyUpstreamByteWithoutDownstreamIsRelay(t *testing.T) {
	tr := New("req-1", 16)
	tr.Mark(StagePreDispatch, SideLocal, MilestoneDispatch, true, "")
	tr.Mark(StageUpstreamWaitHeaders, SideUpstream, MilestoneHeaders, true, "")
	tr.Mark(StageUpstreamRead, SideUpstream, MilestoneFirstByte, true, "")
	tr.Mark(StageRelayTransform, SideRelay, "", false, "malformed_frame")
	got := tr.Classify()
	if got.Side != SideRelay || got.Stage != StageRelayTransform {
		t.Fatalf("got=%#v", got)
	}
}

func TestClassifyDownstreamThenUpstreamReadTerminalIsMidStreamUpstream(t *testing.T) {
	tr := New("req-1", 16)
	tr.Mark(StagePreDispatch, SideLocal, MilestoneDispatch, true, "")
	tr.Mark(StageDownstreamWrite, SideDownstream, MilestoneFirstDownstream, true, "")
	tr.Mark(StageUpstreamRead, SideUpstream, "", false, "stream_reset")
	got := tr.Classify()
	if got.Side != SideUpstream || got.Stage != StageUpstreamRead {
		t.Fatalf("got=%#v", got)
	}
}

func TestClassifyUpstreamCompleteDownstreamIncompleteIsClientPath(t *testing.T) {
	tr := New("req-1", 16)
	tr.Mark(StageUpstreamRead, SideUpstream, MilestoneUpstreamEnd, true, "")
	tr.Mark(StageDownstreamWrite, SideDownstream, "", false, "client_disconnect")
	got := tr.Classify()
	if got.Side != SideDownstream || got.Stage != StageDownstreamWrite {
		t.Fatalf("got=%#v", got)
	}
}

func TestRecordIsBoundedAndNeverBlocksDelivery(t *testing.T) {
	tr := New("req-1", 2)
	tr.Mark(StagePreDispatch, SideLocal, MilestoneDispatch, true, "")
	tr.Mark(StageUpstreamWaitHeaders, SideUpstream, MilestoneHeaders, true, "")
	tr.Mark(StageUpstreamRead, SideUpstream, MilestoneFirstByte, true, "")
	if len(tr.Events()) != 2 {
		t.Fatalf("events=%d", len(tr.Events()))
	}
}

func TestElapsedIsFromAdmitAndMissingTelemetryIsSafe(t *testing.T) {
	var tr *Trace
	tr.Mark(StagePreDispatch, SideLocal, MilestoneDispatch, true, "")
	if tr.Classify().Stage != "" {
		t.Fatal("nil trace must not invent attribution")
	}
	live := New("req-1", 8)
	time.Sleep(time.Millisecond)
	live.Mark(StagePreDispatch, SideLocal, MilestoneDispatch, true, "")
	ev := live.Events()[0]
	if ev.Elapsed < time.Millisecond {
		t.Fatalf("elapsed=%s", ev.Elapsed)
	}
}
