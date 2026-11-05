package cli

import (
	"time"

	"github.com/brudil/workspace/internal/crew"
	"github.com/brudil/workspace/internal/mission"
)

// UseLocalDispatchHost runs dispatched crew as children of the test instead
// of in a deck server, and returns a cleanup that stops them. Unlike a real
// in-process host, leaving it doesn't stop them (the dispatch command leaves
// as soon as the crew has its prompt), so tests can watch them work.
func UseLocalDispatchHost(root string) func() {
	old := dispatchHost
	h := &keepRunningHost{LocalHost: &crew.LocalHost{Store: mission.Store{Root: root}}}
	dispatchHost = func(string) crew.Host { return h }
	return func() {
		dispatchHost = old
		for _, t := range h.terms {
			t.Close(time.Second)
		}
		h.LocalHost.Leave()
	}
}

type keepRunningHost struct {
	*crew.LocalHost
	terms []crew.Term
	specs []crew.Spec
}

func (h *keepRunningHost) Launch(s crew.Spec) (crew.Term, error) {
	t, err := h.LocalHost.Launch(s)
	if err == nil {
		h.terms = append(h.terms, t)
		h.specs = append(h.specs, s)
	}
	return t, err
}

// Running lists the crew it launched that are still running, as the deck
// server does, so a later dispatch can reach them.
func (h *keepRunningHost) Running(skip func(key string) bool) []crew.Hosted {
	var out []crew.Hosted
	for i, t := range h.terms {
		if t.Exited() || (skip != nil && skip(h.specs[i].Key())) {
			continue
		}
		out = append(out, crew.Hosted{Spec: h.specs[i], Term: t})
	}
	return out
}

func (h *keepRunningHost) Leave() {}
