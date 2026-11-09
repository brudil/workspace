package web

import (
	"context"
	"sync"
	"time"
)

// hub tells open pages when what they show has changed. While any page is
// open it checks every poll interval; with none, it doesn't look.
type hub struct {
	poll        time.Duration
	fingerprint func() string

	mu   sync.Mutex
	subs map[chan string]struct{}
	last string
}

func newHub(poll time.Duration, fingerprint func() string) *hub {
	return &hub{poll: poll, fingerprint: fingerprint, subs: map[chan string]struct{}{}}
}

// subscribe returns a channel of event names, and a function to stop. The
// page fetches what it shows after subscribing (on the stream's hello), so
// anything that changes after this baseline reaches it.
func (h *hub) subscribe() (<-chan string, func()) {
	ch := make(chan string, 8)
	var baseline string
	if !h.watching() {
		baseline = h.fingerprint()
	}
	h.mu.Lock()
	if len(h.subs) == 0 {
		h.last = baseline
	}
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		delete(h.subs, ch)
		h.mu.Unlock()
	}
}

func (h *hub) watching() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs) > 0
}

func (h *hub) run(ctx context.Context) {
	t := time.NewTicker(h.poll)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if !h.watching() {
			continue
		}
		fp := h.fingerprint()
		h.mu.Lock()
		changed := fp != h.last
		h.last = fp
		if changed {
			for ch := range h.subs {
				select {
				case ch <- "workspace":
				default: // a page that's this far behind will refetch anyway
				}
			}
		}
		h.mu.Unlock()
	}
}
