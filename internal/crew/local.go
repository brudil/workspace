package crew

import (
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	"github.com/brudil/workspace/internal/mission"
	"github.com/brudil/workspace/internal/termpane"
)

// LeaveGrace is how long crew get to exit after SIGHUP when an in-process
// host stops them.
const LeaveGrace = 3 * time.Second

// LocalHost runs crew as children of this process, so they stop when it
// exits. The deck server is the normal host; this one is for tests and
// WS_DECK_SERVER=0. It records ends exactly as the server does.
type LocalHost struct {
	Store   mission.Store
	OnEnded EndHook

	mu      sync.Mutex
	terms   []*localTerm
	ending  sync.WaitGroup
	leaving atomic.Bool
}

// localTerm is a pane whose Done (and Exited) wait for its end to be
// recorded, so nobody sees a crew exit before the mission says so.
type localTerm struct {
	*termpane.Pane
	stopped atomic.Bool
	done    chan struct{}
	copies  atomic.Uint64 // copies already taken
}

func (t *localTerm) TakeCopied() (string, bool) {
	n, text := t.Pane.Copied()
	if t.copies.Swap(n) == n {
		return "", false
	}
	return text, true
}

func (t *localTerm) Done() <-chan struct{} { return t.done }

func (t *localTerm) Exited() bool {
	select {
	case <-t.done:
		return true
	default:
		return false
	}
}

func (t *localTerm) Close(grace time.Duration) {
	t.stopped.Store(true)
	t.Pane.Close(grace)
}

func (h *LocalHost) Launch(s Spec) (Term, error) {
	cmd := exec.Command(s.Args[0], s.Args[1:]...)
	cmd.Dir = s.Dir
	cmd.Env = s.Env
	p, err := termpane.Start(cmd, s.Width, s.Height)
	if err != nil {
		return nil, err
	}
	t := &localTerm{Pane: p, done: make(chan struct{})}
	h.mu.Lock()
	h.terms = append(h.terms, t)
	h.mu.Unlock()
	h.ending.Add(1)
	go func() {
		defer h.ending.Done()
		defer close(t.done)
		<-p.Done()
		exit, leaving := p.ExitSummary(), h.leaving.Load()
		if leaving {
			exit = "stopped: " + exit
		}
		Finish(h.Store, s, exit, p.Transcript(), leaving || t.stopped.Load(), h.OnEnded)
	}()
	return t, nil
}

func (h *LocalHost) Running(func(string) bool) []Hosted { return nil }

func (h *LocalHost) Detachable() bool { return false }

// Leave stops every crew still running and waits until all their ends are
// recorded.
func (h *LocalHost) Leave() {
	h.leaving.Store(true)
	h.mu.Lock()
	terms := append([]*localTerm(nil), h.terms...)
	h.mu.Unlock()
	var wg sync.WaitGroup
	for _, t := range terms {
		wg.Add(1)
		go func() {
			defer wg.Done()
			t.Pane.Close(LeaveGrace)
		}()
	}
	wg.Wait()
	h.ending.Wait()
}
