package kiro

import (
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
)

func TestValidateCapabilitiesAcceptsParallelToolCallsHint(t *testing.T) {
	yes := true
	if err := ValidateCapabilities(protocol.ParsedRequest{Options: protocol.RequestOptions{ParallelToolCalls: &yes}}); err != nil {
		t.Fatalf("parallel hint should be accepted: %v", err)
	}
}

func TestValidateCapabilitiesRejectsUnsupportedShapes(t *testing.T) {
	tier := "priority"
	if err := ValidateCapabilities(protocol.ParsedRequest{Options: protocol.RequestOptions{ServiceTier: &tier}}); err == nil || !strings.Contains(err.Error(), "service") {
		t.Fatalf("tier=%v", err)
	}
	if err := ValidateCapabilities(protocol.ParsedRequest{Options: protocol.RequestOptions{TextFormat: &protocol.TextFormat{Type: "json_schema"}}}); err == nil {
		t.Fatal("schema")
	}
	if err := ValidateCapabilities(protocol.ParsedRequest{}); err != nil {
		t.Fatal(err)
	}
}
