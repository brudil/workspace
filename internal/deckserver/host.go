package deckserver

import (
	"sync"

	"github.com/brudil/workspace/internal/crew"
)

// Host runs crew in the workspace's deck server, as a crew.Host. It only
// starts the server when crew are first launched; looking for running crew
// just looks for a server.
type Host struct {
	root string

	mu     sync.Mutex
	client *Client
}

var _ crew.Host = (*Host)(nil)

// NewHost is a Host for the workspace at root.
func NewHost(root string) *Host { return &Host{root: root} }

func (h *Host) connect(start bool) (*Client, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.client != nil {
		select {
		case <-h.client.Lost():
			h.client = nil
		default:
			return h.client, nil
		}
	}
	var c *Client
	var err error
	if start {
		c, err = Connect(h.root)
	} else {
		c, err = Dial(h.root)
	}
	if err != nil {
		return nil, err
	}
	h.client = c
	return c, nil
}

func (h *Host) Launch(s crew.Spec) (crew.Term, error) {
	c, err := h.connect(true)
	if err != nil {
		return nil, err
	}
	return c.Launch(s)
}

func (h *Host) Running(skip func(key string) bool) []crew.Hosted {
	c, err := h.connect(false)
	if err != nil {
		return nil // no server running: nothing to attach to
	}
	infos, err := c.List()
	if err != nil {
		return nil
	}
	var out []crew.Hosted
	for _, info := range infos {
		if skip != nil && skip(info.Spec.Key()) {
			continue
		}
		t, err := c.Attach(info)
		if err != nil {
			continue
		}
		out = append(out, crew.Hosted{Spec: info.Spec, Term: t})
	}
	return out
}

func (h *Host) Detachable() bool { return true }

// Flush waits until everything sent so far has reached the server: requests
// go out in order, so one round trip covers them.
func (h *Host) Flush() {
	if c, err := h.connect(false); err == nil {
		c.List()
	}
}

// Leave disconnects; crew keep running on the server.
func (h *Host) Leave() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.client != nil {
		h.client.Close()
		h.client = nil
	}
}
