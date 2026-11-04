package cli

import (
	"sync"
	"sync/atomic"
)

// crewRegistry holds every crew run the deck knows about. It is shared by
// all copies of the (value-typed) model, so code running after the program
// ends still sees every run.
type crewRegistry struct {
	mu   sync.Mutex
	runs []*deckCrewRun
	// adopting is set while a look for crew launched elsewhere is in flight.
	adopting atomic.Bool
}

// liveKey reports whether a live run has the slug/crew key. Like everything
// that reads a run's state, call it on the deck's own goroutine; background
// work gets a snapshot from liveKeys.
func (g *crewRegistry) liveKey(key string) bool {
	return g.liveKeys()[key]
}

// liveKeys is a snapshot of the live runs' slug/crew keys.
func (g *crewRegistry) liveKeys() map[string]bool {
	keys := map[string]bool{}
	for _, r := range g.live("") {
		keys[r.slug+"/"+r.crewID] = true
	}
	return keys
}

func (g *crewRegistry) add(r *deckCrewRun) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.runs = append(g.runs, r)
}

func (g *crewRegistry) all() []*deckCrewRun {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]*deckCrewRun(nil), g.runs...)
}

// live returns the live runs for a mission ("" for every mission), in
// launch order.
func (g *crewRegistry) live(slug string) []*deckCrewRun {
	var out []*deckCrewRun
	for _, r := range g.all() {
		if r.live() && (slug == "" || r.slug == slug) {
			out = append(out, r)
		}
	}
	return out
}
