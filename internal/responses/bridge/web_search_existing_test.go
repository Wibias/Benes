package bridge

import (
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
)

func TestWebSearchCellAndNextMessageCitationBinding(t *testing.T) {
	ids := []string{"ws_1", "msg_1"}
	b := New("gpt-5", Options{ResponseID: "resp_1", ID: func(string) string {
		id := ids[0]
		ids = ids[1:]
		return id
	}})
	_ = b.Start()
	begin := mustHandle(t, b, protocol.Event{Type: protocol.EventWebSearchCallBegin, ID: "search_1"})
	assertFrameTypes(t, begin, []string{"response.output_item.added"})
	end := mustHandle(t, b, protocol.Event{
		Type:    protocol.EventWebSearchCallEnd,
		ID:      "search_1",
		Status:  "completed",
		Queries: []string{"benes network"},
		Sources: []protocol.URLCitation{{URL: "https://example.com/a", Title: "Example A"}},
	})
	assertFrameTypes(t, end, []string{"response.output_item.done"})
	searchItem := object(t, end[0].Data["item"])
	action := object(t, searchItem["action"])
	if action["query"] != "benes network" {
		t.Fatalf("action=%#v", action)
	}
	queries := array(t, action["queries"])
	if len(queries) != 1 || queries[0] != "benes network" {
		t.Fatalf("queries=%#v", queries)
	}

	_ = mustHandle(t, b, protocol.Event{Type: protocol.EventTextDelta, Text: "result"})
	closed := mustHandle(t, b, protocol.Event{Type: protocol.EventDone})
	var message map[string]any
	for _, frame := range closed {
		if frame.Name == "response.output_item.done" {
			candidate := object(t, frame.Data["item"])
			if candidate["type"] == "message" {
				message = candidate
			}
		}
	}
	if message == nil {
		t.Fatal("message output item was not closed")
	}
	content := array(t, message["content"])
	annotations := array(t, object(t, content[0])["annotations"])
	if len(annotations) != 1 {
		t.Fatalf("annotations=%#v", annotations)
	}
	annotation := object(t, annotations[0])
	if annotation["type"] != "url_citation" || annotation["url"] != "https://example.com/a" || annotation["title"] != "Example A" {
		t.Fatalf("annotation=%#v", annotation)
	}
	if number(t, annotation["start_index"]) != 0 || number(t, annotation["end_index"]) != 0 {
		t.Fatalf("annotation indexes=%#v", annotation)
	}
}

func TestWebSearchBatchActionCarriesSingularQuery(t *testing.T) {
	b := New("gpt-5", Options{ResponseID: "resp_1", ID: func(string) string { return "ws_1" }})
	_ = b.Start()
	_ = mustHandle(t, b, protocol.Event{Type: protocol.EventWebSearchCallBegin, ID: "s"})
	frames := mustHandle(t, b, protocol.Event{Type: protocol.EventWebSearchCallEnd, ID: "s", Queries: []string{"one", "two"}})
	item := object(t, frames[0].Data["item"])
	action := object(t, item["action"])
	if action["query"] != "one" {
		t.Fatalf("batch action missing singular query: %#v", action)
	}
	if len(array(t, action["queries"])) != 2 {
		t.Fatalf("action=%#v", action)
	}
}

func TestWebSearchEndWithoutMatchingBeginSynthesizesCell(t *testing.T) {
	b := New("gpt-5", Options{ResponseID: "resp_1", ID: func(string) string { return "ws_1" }})
	_ = b.Start()
	frames := mustHandle(t, b, protocol.Event{Type: protocol.EventWebSearchCallEnd, ID: "missing", Queries: []string{"q"}})
	assertFrameTypes(t, frames, []string{"response.output_item.added", "response.output_item.done"})
}

func TestOpenWebSearchFailsOnIncompleteTerminal(t *testing.T) {
	b := New("gpt-5", Options{ResponseID: "resp_1", ID: func(string) string { return "ws_1" }})
	_ = b.Start()
	_ = mustHandle(t, b, protocol.Event{Type: protocol.EventWebSearchCallBegin, ID: "s"})
	frames := mustHandle(t, b, protocol.Event{Type: protocol.EventIncomplete, Reason: "upstream_stall"})
	assertFrameTypes(t, frames, []string{"response.output_item.done", "response.incomplete"})
	if object(t, frames[0].Data["item"])["status"] != "failed" {
		t.Fatalf("item=%#v", frames[0].Data["item"])
	}
}

func TestSearchSourcesDeduplicateByURLBeforeBinding(t *testing.T) {
	ids := []string{"ws_1", "ws_2", "msg_1"}
	b := New("gpt-5", Options{ResponseID: "resp_1", ID: func(string) string {
		id := ids[0]
		ids = ids[1:]
		return id
	}})
	_ = b.Start()
	_ = mustHandle(t, b, protocol.Event{Type: protocol.EventWebSearchCallBegin, ID: "s1"})
	_ = mustHandle(t, b, protocol.Event{Type: protocol.EventWebSearchCallEnd, ID: "s1", Sources: []protocol.URLCitation{{URL: "https://example.com", Title: "first"}}})
	_ = mustHandle(t, b, protocol.Event{Type: protocol.EventWebSearchCallBegin, ID: "s2"})
	_ = mustHandle(t, b, protocol.Event{Type: protocol.EventWebSearchCallEnd, ID: "s2", Sources: []protocol.URLCitation{{URL: "https://example.com", Title: "second"}}})
	_ = mustHandle(t, b, protocol.Event{Type: protocol.EventTextDelta, Text: "x"})
	frames := mustHandle(t, b, protocol.Event{Type: protocol.EventDone})
	item := object(t, frames[2].Data["item"])
	content := array(t, item["content"])
	annotations := array(t, object(t, content[0])["annotations"])
	if len(annotations) != 1 {
		t.Fatalf("annotations=%#v", annotations)
	}
	if object(t, annotations[0])["title"] != "first" {
		t.Fatalf("first source metadata must win: %#v", annotations[0])
	}
}

func TestDoneMaxTokensProducesIncomplete(t *testing.T) {
	b := New("gpt-5", Options{ResponseID: "resp_1"})
	_ = b.Start()
	frames := mustHandle(t, b, protocol.Event{Type: protocol.EventDone, StopReason: "max_tokens"})
	assertFrameTypes(t, frames, []string{"response.incomplete"})
	response := object(t, frames[0].Data["response"])
	if object(t, response["incomplete_details"])["reason"] != "max_output_tokens" {
		t.Fatalf("response=%#v", response)
	}
}

func TestDoneContentFilterProducesIncomplete(t *testing.T) {
	b := New("gpt-5", Options{ResponseID: "resp_1"})
	_ = b.Start()
	frames := mustHandle(t, b, protocol.Event{Type: protocol.EventDone, StopReason: "content_filter"})
	assertFrameTypes(t, frames, []string{"response.incomplete"})
	response := object(t, frames[0].Data["response"])
	if object(t, response["incomplete_details"])["reason"] != "content_filter" {
		t.Fatalf("response=%#v", response)
	}
}
