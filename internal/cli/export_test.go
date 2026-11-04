package cli

import "time"

// UseLocalDispatchHost runs dispatched crew as children of the test instead
// of in a deck server, and returns a cleanup that stops them.
func UseLocalDispatchHost() func() {
	old := dispatchHost
	h := &recordingHost{}
	dispatchHost = func(string) crewHost { return h }
	return func() {
		dispatchHost = old
		for _, t := range h.terms {
			t.Close(time.Second)
		}
	}
}

type recordingHost struct {
	localHost
	terms []crewTerm
}

func (h *recordingHost) Launch(s crewSpec) (crewTerm, error) {
	t, err := h.localHost.Launch(s)
	if err == nil {
		h.terms = append(h.terms, t)
	}
	return t, err
}
