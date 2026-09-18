package server

import (
	"strings"
	"sync"

	"github.com/Wibias/Benes/internal/grok"
	"github.com/Wibias/Benes/internal/harnessboard"
)

/*
Grok projection lifecycle.

The listener owns exactly one Grok writer: writeGrokProjection. Three triggers reach it — the
Harness apply, the explicit apply route, and automatic reconciliation — and all three hand it the
same two things: the listener's catalogue and an address. A trigger is not a writer, so no path can
introduce a second implementation or a narrower model set.

Automatic reconciliation runs on the two events that can make an *applied* projection stale:

  - the live catalogue changing (the discovery sync and the synthesized reload are the only code
    paths that replace h.catalogModels while running);
  - the data plane coming up on an address, which is what makes a previously written base URL
    stale.

It never creates a projection that was not applied. Enabling Grok stays the user's decision through
Harness apply; reconciliation only keeps an existing managed region true. Failure is fail-closed:
the projection is left as it is, the failure is recorded for the owning route, and the listener
keeps serving.
*/

// projectionLifecycle is the address the data plane bound plus the last automatic failure. Both
// are read from request handlers and written from the serve lifecycle, so they are guarded.
type projectionLifecycle struct {
	mu        sync.Mutex
	hostname  string
	port      int
	lastError string
}

// writeGrokProjection is the canonical Grok write. Every trigger goes through it.
func (h *handler) writeGrokProjection(hostname string, port int) grok.Result {
	return grok.Inject(port, h.grokModels(), grok.InjectOptions{Hostname: hostname})
}

func (h *handler) recordProjectionError(message string) {
	h.projections.mu.Lock()
	defer h.projections.mu.Unlock()
	h.projections.lastError = strings.TrimSpace(message)
}

// lastProjectionError reports the last automatic projection failure, if any.
func (h *handler) lastProjectionError() string {
	h.projections.mu.Lock()
	defer h.projections.mu.Unlock()
	return h.projections.lastError
}

// ReconcileAfterListen is the post-listen lifecycle seam: it records the address the data plane
// actually bound and reconciles every projection the listener owns against it.
func (h *handler) ReconcileAfterListen(hostname string, port int) {
	if h == nil || port <= 0 {
		return
	}
	h.projections.mu.Lock()
	h.projections.hostname = strings.TrimSpace(hostname)
	h.projections.port = port
	h.projections.mu.Unlock()
	h.reconcileAppliedProjections()
}

func (h *handler) boundAddress() (string, int) {
	h.projections.mu.Lock()
	defer h.projections.mu.Unlock()
	return h.projections.hostname, h.projections.port
}

// notifyCatalogueChanged is the single hook a live catalogue mutation calls, so the
// post-mutation integration work is declared in one place instead of scattered through the
// functions that happen to rebuild the catalogue.
func (h *handler) notifyCatalogueChanged() {
	h.reconcileAppliedProjections()
}

// reconcileAppliedProjections reconciles every applied projection the listener owns. Grok is the
// only carrier today; a second one would be reconciled here, not in a second lifecycle.
func (h *handler) reconcileAppliedProjections() {
	h.reconcileGrokProjection()
}

// grokAutoApply reads the stored Harness setting. Unreadable or absent settings fall back to the
// documented default, and an unreadable settings file fails closed: the user's config is not
// rewritten on a guess.
func (h *handler) grokAutoApply() bool {
	home := strings.TrimSpace(h.benesHome())
	if home == "" {
		return false
	}
	all, err := harnessboard.LoadSettings(home)
	if err != nil {
		return false
	}
	if row, ok := all["grok"]; ok {
		return row.AutoApply
	}
	return harnessboard.DefaultSettings().AutoApply
}

// reconcileGrokProjection keeps an applied Grok projection current.
func (h *handler) reconcileGrokProjection() {
	hostname, port := h.boundAddress()
	if port <= 0 {
		// No bound address: this process never started a listener, so there is nothing the
		// projection could truthfully point at.
		return
	}
	if !h.grokAutoApply() {
		// The Harness is set to apply changes manually. Leaving the block alone is the point:
		// registration reads as out of date until the user re-applies.
		h.recordProjectionError("")
		return
	}
	if !grok.ReadStatus(grok.InjectOptions{}).Present {
		// Nothing has been applied. Creating the managed region is the Harness apply decision
		// and never something a catalogue change or a restart may take on the user's behalf.
		h.recordProjectionError("")
		return
	}
	result := h.writeGrokProjection(loopbackHost(hostname), port)
	if !result.OK || result.SkippedReason != "" {
		message := strings.TrimSpace(result.Message)
		if message == "" {
			message = "the Grok projection could not be written"
		}
		h.recordProjectionError(message)
		return
	}
	h.recordProjectionError("")
}

// loopbackHost normalizes the address a Grok client can actually reach. A wildcard bind serves
// loopback too, so it becomes 127.0.0.1; a specific non-loopback bind is left alone and the
// writer's own loopback rule decides what happens to the managed region.
func loopbackHost(hostname string) string {
	host := strings.TrimSpace(hostname)
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		return "127.0.0.1"
	default:
		return host
	}
}

