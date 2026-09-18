package google

import (
	"strings"
	"testing"
)

func TestAIStudioURLUsesWireIDAndKeepsHTTPS(t *testing.T) {
	id, err := ResolveIdentity(KindAIStudio, "gemini-3.7-flash", WireRenameDefault)
	if err != nil {
		t.Fatal(err)
	}
	got, err := GenerateURL(Endpoint{Kind: KindAIStudio}, id, true)
	if err != nil || !strings.Contains(got, "/models/gemini-3.7-flash-tiered:streamGenerateContent") {
		t.Fatalf("got=%s err=%v", got, err)
	}
	if strings.Contains(got, "gemini-3.7-flash-tiered") && id.PublicID != "gemini-3.7-flash" {
		t.Fatal("public identity must stay bare")
	}
	if _, err := ResolveAIStudioDestination("http://generativelanguage.googleapis.com"); err == nil {
		t.Fatal("cleartext")
	}
}

func TestVertexURLKeepsRequestedModelAndRejectsBadLocation(t *testing.T) {
	id, err := ResolveIdentity(KindVertex, "gemini-3.7-flash", WireRenameDefault)
	if err != nil {
		t.Fatal(err)
	}
	got, err := GenerateURL(Endpoint{Kind: KindVertex, Project: "proj", Location: "us-central1"}, id, false)
	if err != nil || got != "https://us-central1-aiplatform.googleapis.com/v1/projects/proj/locations/us-central1/publishers/google/models/gemini-3.7-flash:generateContent" {
		t.Fatalf("got=%s err=%v", got, err)
	}
	if _, err := GenerateURL(Endpoint{Kind: KindVertex, Project: "proj", Location: "US-CENTRAL1"}, id, false); err == nil {
		t.Fatal("uppercase location")
	}
}
