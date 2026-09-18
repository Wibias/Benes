package kiro

import (
	"fmt"

	"github.com/Wibias/Benes/internal/protocol"
)

func ValidateCapabilities(parsed protocol.ParsedRequest) error {
	if parsed.Options.ServiceTier != nil && *parsed.Options.ServiceTier != "" {
		return fmt.Errorf("Kiro does not support service tiers")
	}
	if parsed.Options.TextFormat != nil {
		return fmt.Errorf("Kiro does not support structured text format controls")
	}
	if parsed.Options.ToolChoice != nil && parsed.Options.ToolChoice.Kind == protocol.ToolChoiceRequired {
		return fmt.Errorf("Kiro does not support required tool choice")
	}
	return nil
}
