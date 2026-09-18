package cursor

import "testing"

func TestNormalizeToolResultKeepsTextAndErrorTogether(t *testing.T) {
	empty := NormalizeToolResult("", nil, false, false)
	if empty.Text != "Tool produced no output." || empty.IsError {
		t.Fatalf("empty=%#v", empty)
	}
	failed := NormalizeToolResult("", []string{"", ""}, true, false)
	if !failed.IsError || failed.Text != "Tool failed." {
		t.Fatalf("failed=%#v", failed)
	}
	ok := NormalizeToolResult("", []string{"hello"}, false, false)
	if ok.Text != "hello" || ok.IsError {
		t.Fatalf("ok=%#v", ok)
	}
	blob := NormalizeToolResult("", []string{"ignored"}, true, true)
	if !blob.HasBlob || blob.Text != "" {
		t.Fatalf("blob must stay lossless: %#v", blob)
	}
}
