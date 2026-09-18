package server

import (
	"testing"

	"github.com/Wibias/Benes/internal/provideractivity"
)

func TestProviderHealthFailureActiveRequiresExplicitRecovery(t *testing.T) {
	h := &handler{activity: provideractivity.New()}
	h.activity.Record(provideractivity.Event{
		Provider:  "custom",
		Type:      "provider_health_failure",
		Timestamp: 1,
	})
	h.activity.Record(provideractivity.Event{
		Provider:  "custom",
		Type:      "connection_validated",
		Timestamp: 2,
	})

	if !h.providerHealthFailureActive("custom") {
		t.Fatal("connection validation incorrectly cleared active provider health failure")
	}

	h.activity.Record(provideractivity.Event{
		Provider:  "custom",
		Type:      "provider_health_recovered",
		Timestamp: 3,
	})
	if h.providerHealthFailureActive("custom") {
		t.Fatal("explicit provider health recovery did not clear active failure")
	}
}
