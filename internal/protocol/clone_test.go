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
