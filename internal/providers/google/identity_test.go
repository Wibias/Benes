package google

import "testing"

func TestAIStudioDefaultRenamesFlashButKeepsPublicIdentity(t *testing.T) {
	got, err := ResolveIdentity(KindAIStudio, "gemini-3.7-flash", WireRenameDefault)
	if err != nil || got.PublicID != "gemini-3.7-flash" || got.WireID != "gemini-3.7-flash-tiered" {
		t.Fatalf("got=%#v err=%v", got, err)
	}
	got, err = ResolveIdentity(KindAIStudio, "gemini-3.6-flash", WireRenameDefault)
	if err != nil || got.PublicID != "gemini-3.6-flash" || got.WireID != "gemini-3.6-flash-tiered" {
		t.Fatalf("36=%#v err=%v", got, err)
	}
	got, err = ResolveIdentity(KindAIStudio, "gemini-3.5-flash", WireRenameDefault)
	if err != nil || got.WireID != "gemini-3.5-flash" {
		t.Fatalf("35=%#v err=%v", got, err)
	}
}

func TestAIStudioExplicitFalseKeepsBarePickerID(t *testing.T) {
	disabled := false
	got, err := ResolveIdentity(KindAIStudio, "gemini-3.7-flash", ParseWireRenamePolicy(&disabled))
	if err != nil || got.WireID != "gemini-3.7-flash" || got.PublicID != "gemini-3.7-flash" {
		t.Fatalf("got=%#v err=%v", got, err)
	}
}

func TestVertexIgnoresAIStudioRenamePolicy(t *testing.T) {
	got, err := ResolveIdentity(KindVertex, "gemini-3.7-flash", WireRenameDefault)
	if err != nil || got.PublicID != "gemini-3.7-flash" || got.WireID != "gemini-3.7-flash" {
		t.Fatalf("got=%#v err=%v", got, err)
	}
}
