package providers

import "github.com/Wibias/Benes/internal/protocol"

func CloneDispatch(in DispatchRequest) DispatchRequest {
	out := DispatchRequest{
		Parsed:                protocol.CloneParsedRequest(in.Parsed),
		ForwardHeaders:        NewForwardHeadersWithBlockedAuthorization(in.ForwardHeaders.Values(), in.ForwardHeaders.BlockedAuthorization()),
		CodexAccountID:        in.CodexAccountID,
		Turn:                  in.Turn,
		openCodeGoSession:     in.openCodeGoSession,
		PreferCommitted:       in.PreferCommitted,
		ConfiguredServiceTier: in.ConfiguredServiceTier,
	}
	if in.PhysicalPin != nil {
		pin := *in.PhysicalPin
		out.PhysicalPin = &pin
	}
	return out
}
