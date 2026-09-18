package protocol

import "encoding/json"

type minimaxReasoningEventMeta struct {
	MiniMaxReasoningDetail MiniMaxReasoningDetail `json:"minimaxReasoningDetail"`
}

func EncodeMiniMaxReasoningDetail(detail MiniMaxReasoningDetail) json.RawMessage {
	raw, err := json.Marshal(minimaxReasoningEventMeta{MiniMaxReasoningDetail: detail})
	if err != nil {
		return nil
	}
	return raw
}

func MiniMaxReasoningDetailFromEvent(event Event) (MiniMaxReasoningDetail, bool) {
	if len(event.ProviderMetadata) == 0 {
		return MiniMaxReasoningDetail{}, false
	}
	var meta minimaxReasoningEventMeta
	if json.Unmarshal(event.ProviderMetadata, &meta) != nil {
		return MiniMaxReasoningDetail{}, false
	}
	if meta.MiniMaxReasoningDetail.ID == "" && meta.MiniMaxReasoningDetail.Type == "" && meta.MiniMaxReasoningDetail.Text == "" {
		return MiniMaxReasoningDetail{}, false
	}
	return meta.MiniMaxReasoningDetail, true
}
