package server

import (
	"testing"

	"github.com/Wibias/Benes/internal/sidecar/fabric"
)

func TestFabricTerminalIntentSelectOrders(t *testing.T) {
	cases := []struct {
		name   string
		order  []fabricTerminalIntent
		winner fabricTerminalIntent
	}{
		{"complete_then_cancel", []fabricTerminalIntent{fabricIntentComplete, fabricIntentCancel}, fabricIntentComplete},
		{"cancel_then_complete", []fabricTerminalIntent{fabricIntentCancel, fabricIntentComplete}, fabricIntentCancel},
		{"shutdown_then_complete", []fabricTerminalIntent{fabricIntentShutdown, fabricIntentComplete}, fabricIntentShutdown},
		{"complete_then_shutdown", []fabricTerminalIntent{fabricIntentComplete, fabricIntentShutdown}, fabricIntentComplete},
		{"cancel_then_shutdown", []fabricTerminalIntent{fabricIntentCancel, fabricIntentShutdown}, fabricIntentCancel},
		{"shutdown_then_cancel", []fabricTerminalIntent{fabricIntentShutdown, fabricIntentCancel}, fabricIntentShutdown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			live := &fabricLiveRun{id: fabric.RunIdentity{RunID: "run_x", TaskID: "t", Owner: "w", Fence: 1}}
			var first bool
			for i, intent := range tc.order {
				won := live.selectTerminal(intent)
				if i == 0 {
					first = won
					if !won {
						t.Fatal("first select must win")
					}
				} else if won && intent != tc.winner {
					t.Fatalf("later intent %v incorrectly won", intent)
				}
			}
			_ = first
			if live.selectedTerminal() != tc.winner {
				t.Fatalf("winner=%v want %v", live.selectedTerminal(), tc.winner)
			}
		})
	}
}
