package protocol

import "testing"

func TestCloneParsedRequestDeepCopiesMiniMaxReasoningDetails(t *testing.T) {
	index := 0
	original := ParsedRequest{
		Context: Context{Messages: []Message{{
			Role: RoleAssistant,
			Content: []ContentPart{{
				Type:     ContentThinking,
				Thinking: "plan",
				ProviderMetadata: &ProviderOpaqueMetadata{MiniMax: &MiniMaxOpaqueMetadata{
					ReasoningDetails: []MiniMaxReasoningDetail{{
						Type:  "reasoning.text",
						ID:    "rs_1",
						Index: &index,
						Text:  "plan",
					}},
				}},
			}},
		}}},
	}

	cloned := CloneParsedRequest(original)
	clonedMeta := cloned.Context.Messages[0].Content[0].ProviderMetadata
	if clonedMeta == nil || clonedMeta.MiniMax == nil || len(clonedMeta.MiniMax.ReasoningDetails) != 1 {
		t.Fatalf("cloned meta=%#v", clonedMeta)
	}
	clonedMeta.MiniMax.ReasoningDetails[0].Text = "mutated"
	clonedIndex := 9
	clonedMeta.MiniMax.ReasoningDetails[0].Index = &clonedIndex

	got := original.Context.Messages[0].Content[0].ProviderMetadata.MiniMax.ReasoningDetails[0]
	if got.Text != "plan" {
		t.Fatalf("original text mutated: %#v", got)
	}
	if got.Index == nil || *got.Index != 0 {
		t.Fatalf("original index mutated: %#v", got.Index)
	}
}

func TestCloneParsedRequestPreservesFileCarrierFields(t *testing.T) {
	original := ParsedRequest{
		Context: Context{Messages: []Message{{
			Role: RoleUser,
			Content: []ContentPart{
				{Type: ContentImage, FileID: "file-img", Detail: "high"},
				{Type: ContentFile, FileID: "file-doc", Filename: "doc.pdf"},
				{Type: ContentFile, FileData: "ZGF0YQ==", Filename: "inline.txt"},
			},
		}}},
	}

	cloned := CloneParsedRequest(original)
	parts := cloned.Context.Messages[0].Content
	if len(parts) != 3 {
		t.Fatalf("parts=%#v", parts)
	}
	if parts[0].Type != ContentImage || parts[0].FileID != "file-img" || parts[0].Detail != "high" {
		t.Fatalf("image=%#v", parts[0])
	}
	if parts[1].Type != ContentFile || parts[1].FileID != "file-doc" || parts[1].Filename != "doc.pdf" {
		t.Fatalf("file=%#v", parts[1])
	}
	if parts[2].Type != ContentFile || parts[2].FileData != "ZGF0YQ==" || parts[2].Filename != "inline.txt" {
		t.Fatalf("inline=%#v", parts[2])
	}
}
