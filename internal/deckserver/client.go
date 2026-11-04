package deckserver

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

func uvWheel(m uv.Mouse) uv.MouseEvent { return uv.MouseWheelEvent(m) }

// Client is one deck's connection to its workspace's server.
type Client struct {
	nc    net.Conn
	enc   *json.Encoder
	queue chan request // every request, in order, to the one writer

	mu      sync.Mutex
	nextID  int
	pending map[int]chan message
	terms   map[string]*Term
	closed  bool
	lost    chan struct{} // closed when the connection drops

	serverVer, serverPid int
}

// ServerPid is the server's process id.
func (c *Client) ServerPid() int { return c.serverPid }

// Compatible reports whether the server speaks this ws's protocol.
func (c *Client) Compatible() bool { return c.serverVer == ProtocolVersion }

// Dial connects to a running server and checks it speaks our protocol.
func Dial(root string) (*Client, error) { return dial(root, true) }

// DialAny connects without the protocol check, for the ops every version
// keeps stable (hello, shutdown): stopping a server left by another ws.
func DialAny(root string) (*Client, error) { return dial(root, false) }

func dial(root string, checkVersion bool) (*Client, error) {
	nc, err := net.DialTimeout("unix", SocketPath(root), 2*time.Second)
	if err != nil {
		return nil, err
	}
	c := &Client{nc: nc, enc: json.NewEncoder(nc), queue: make(chan request, 4096), pending: map[int]chan message{}, terms: map[string]*Term{}, lost: make(chan struct{})}
	go c.read()
	go c.write()
	reply, err := c.call(request{Op: "hello", Ver: ProtocolVersion})
	if err != nil {
		c.Close()
		return nil, err
	}
	c.serverVer, c.serverPid = reply.Ver, reply.Pid
	if checkVersion && reply.Ver != ProtocolVersion {
		c.Close()
		return nil, fmt.Errorf("the deck server running for this workspace (pid %d) speaks protocol %d, this ws speaks %d: run `ws deck server stop` and try again", reply.Pid, reply.Ver, ProtocolVersion)
	}
	return c, nil
}

// Connect dials the workspace's server, starting one if none is running.
func Connect(root string) (*Client, error) {
	if c, err := Dial(root); err == nil {
		return c, nil
	} else if !IsNoServer(err) {
		return nil, err
	}
	if err := spawn(root); err != nil {
		return nil, fmt.Errorf("starting deck server: %w", err)
	}
	var lastErr error
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		c, err := Dial(root)
		if err == nil {
			return c, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("deck server didn't come up (see %s): %w", LogPath(root), lastErr)
}

// IsNoServer reports whether a Dial error means no server is listening (as
// opposed to one that is there but can't be talked to).
func IsNoServer(err error) bool {
	return errors.Is(err, syscall.ENOENT) || errors.Is(err, syscall.ECONNREFUSED) || os.IsNotExist(err)
}

// spawn starts `<this binary> deck server run --root <root>` in its own
// session, so it outlives the deck and the terminal it was started from.
func spawn(root string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	os.MkdirAll(dirOf(LogPath(root)), 0o755)
	logf, err := os.OpenFile(LogPath(root), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer logf.Close()
	cmd := exec.Command(self, "deck", "server", "run", "--root", root)
	// The server outlives whoever started it (a deck in a capsule, or the
	// orchestrator): none of their mission identity applies to it.
	cmd.Env = append(os.Environ(), "WS_MISSION=", "WS_CREW=", "WS_ROLE=", "WS_ORIGIN_PWD=")
	cmd.Stdin = nil
	cmd.Stdout, cmd.Stderr = logf, logf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

func (c *Client) read() {
	defer func() {
		c.mu.Lock()
		c.closed = true
		for _, ch := range c.pending {
			close(ch)
		}
		c.pending = map[int]chan message{}
		terms := c.terms
		c.mu.Unlock()
		close(c.lost)
		for _, t := range terms {
			t.finish("deck server connection lost")
		}
	}()
	sc := bufio.NewScanner(c.nc)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		var m message
		if json.Unmarshal(sc.Bytes(), &m) != nil {
			continue
		}
		if m.Ev != "" {
			c.event(m)
			continue
		}
		c.mu.Lock()
		ch := c.pending[m.ID]
		delete(c.pending, m.ID)
		c.mu.Unlock()
		if ch != nil {
			ch <- m
		}
	}
}

func (c *Client) event(m message) {
	c.mu.Lock()
	t := c.terms[m.Crew]
	c.mu.Unlock()
	if t == nil {
		return
	}
	switch m.Ev {
	case "frame":
		t.setFrame(m.Frame)
	case "raw":
		t.mirrorRaw(m.Data)
	case "exit":
		t.finish(m.Exit)
		c.mu.Lock()
		delete(c.terms, m.Crew)
		c.mu.Unlock()
	}
}

// write sends queued requests in order. The server handles a connection's
// requests one at a time, so input arrives in the order it was typed.
func (c *Client) write() {
	for {
		select {
		case req := <-c.queue:
			if c.enc.Encode(req) != nil {
				c.nc.Close()
				return
			}
		case <-c.lost:
			return
		}
	}
}

// enqueue numbers a request and queues it, registering ch for its reply.
func (c *Client) enqueue(req request, ch chan message) error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return errors.New("deck server connection closed")
	}
	c.nextID++
	req.ID = c.nextID
	if ch != nil {
		c.pending[req.ID] = ch
	}
	c.mu.Unlock()
	select {
	case c.queue <- req:
		return nil
	case <-c.lost:
		return errors.New("deck server connection closed")
	}
}

// call sends a request and waits for its reply.
func (c *Client) call(req request) (message, error) {
	ch := make(chan message, 1)
	if err := c.enqueue(req, ch); err != nil {
		return message{}, err
	}
	select {
	case m, ok := <-ch:
		if !ok {
			return message{}, errors.New("deck server connection closed")
		}
		if m.Error != "" {
			return m, errors.New(m.Error)
		}
		return m, nil
	case <-time.After(30 * time.Second):
		return message{}, errors.New("deck server didn't reply")
	}
}

// send queues a request without waiting for its reply (input, resizes).
func (c *Client) send(req request) {
	c.enqueue(req, nil)
}

// Close disconnects. Crew keep running on the server.
func (c *Client) Close() error { return c.nc.Close() }

// Lost is closed when the connection to the server drops.
func (c *Client) Lost() <-chan struct{} { return c.lost }

// List returns crew running on the server.
func (c *Client) List() ([]CrewInfo, error) {
	m, err := c.call(request{Op: "list"})
	return m.Crews, err
}

// Shutdown asks the server to stop all crew and exit.
func (c *Client) Shutdown() error {
	_, err := c.call(request{Op: "shutdown"})
	return err
}

// Launch starts crew on the server and attaches to it.
func (c *Client) Launch(spec Spec) (*Term, error) {
	t, err := c.newTerm(spec)
	if err != nil {
		return nil, err
	}
	m, err := c.call(request{Op: "launch", Spec: &spec})
	if err != nil {
		c.dropTerm(spec.Key())
		return nil, err
	}
	t.pid = m.Pid
	if _, err := c.call(request{Op: "attach", Crew: spec.Key()}); err != nil {
		return nil, err
	}
	return t, nil
}

// Attach follows crew already running on the server. It refuses crew this
// client already follows (being launched, or attached before): one Term per
// crew, so frames and the exit can't go to a copy nobody is watching.
func (c *Client) Attach(info CrewInfo) (*Term, error) {
	t, err := c.newTerm(info.Spec)
	if err != nil {
		return nil, err
	}
	t.pid = info.Pid
	if _, err := c.call(request{Op: "attach", Crew: info.Spec.Key()}); err != nil {
		c.dropTerm(info.Spec.Key())
		return nil, err
	}
	return t, nil
}

// ErrFollowing is returned for crew this client already has a Term for.
var ErrFollowing = errors.New("already following that crew")

func (c *Client) newTerm(spec Spec) (*Term, error) {
	t := &Term{c: c, key: spec.Key(), updates: make(chan struct{}, 1), done: make(chan struct{}), w: spec.Width, h: spec.Height}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.terms[spec.Key()]; ok {
		return nil, fmt.Errorf("%w: %s", ErrFollowing, spec.Key())
	}
	c.terms[spec.Key()] = t
	return t, nil
}

func (c *Client) dropTerm(key string) {
	c.mu.Lock()
	delete(c.terms, key)
	c.mu.Unlock()
}

// Term is crew running on the server, seen from a deck. It mirrors the
// methods of termpane.Pane so the deck can use either.
type Term struct {
	c   *Client
	key string
	pid int

	updates chan struct{}
	done    chan struct{}

	mu       sync.Mutex
	frame    Frame
	offset   int // scroll offset we asked the server for
	w, h     int
	exit     string
	ended    bool
	mirror   io.Writer
	doneOnce sync.Once
}

func (t *Term) setFrame(f *Frame) {
	if f == nil {
		return
	}
	t.mu.Lock()
	t.frame = *f
	t.mu.Unlock()
	t.notify()
}

func (t *Term) notify() {
	select {
	case t.updates <- struct{}{}:
	default:
	}
}

func (t *Term) finish(exit string) {
	t.doneOnce.Do(func() {
		t.mu.Lock()
		t.exit, t.ended = exit, true
		t.mu.Unlock()
		close(t.done)
		t.notify()
	})
}

func (t *Term) mirrorRaw(p []byte) {
	t.mu.Lock()
	w := t.mirror
	t.mu.Unlock()
	if w != nil {
		w.Write(p)
	}
}

// RenderScrolled returns the latest frame, asking the server for another
// offset if it differs. The new frame arrives as an update.
func (t *Term) RenderScrolled(offset int) string {
	t.mu.Lock()
	ask := offset != t.offset
	t.offset = offset
	screen := t.frame.Screen
	t.mu.Unlock()
	if ask && !t.Exited() {
		t.c.send(request{Op: "scroll", Crew: t.key, Offset: offset})
	}
	return screen
}

func (t *Term) ScrollbackLen() int { t.mu.Lock(); defer t.mu.Unlock(); return t.frame.ScrollbackLen }
func (t *Term) AltScreen() bool    { t.mu.Lock(); defer t.mu.Unlock(); return t.frame.AltScreen }
func (t *Term) Title() string      { t.mu.Lock(); defer t.mu.Unlock(); return t.frame.Title }

// Text is the latest live frame as plain text, trailing blanks trimmed.
func (t *Term) Text() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	lines := strings.Split(ansi.Strip(t.frame.Screen), "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " ")
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n")
}

func (t *Term) Cursor() (int, int, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.frame.CursorX, t.frame.CursorY, t.frame.CursorVisible
}

// Size is the size last asked for (the server applies it asynchronously).
func (t *Term) Size() (int, int) { t.mu.Lock(); defer t.mu.Unlock(); return t.w, t.h }

func (t *Term) SendKey(k uv.KeyPressEvent) {
	t.c.send(request{Op: "key", Crew: t.key, Key: &k})
}

// SendMouse forwards wheel events (the only kind the deck forwards).
func (t *Term) SendMouse(m uv.MouseEvent) {
	mouse := m.Mouse()
	t.c.send(request{Op: "mouse", Crew: t.key, Mouse: &mouse})
}

func (t *Term) Paste(s string) { t.c.send(request{Op: "paste", Crew: t.key, Text: s}) }

// WriteRaw sends bytes unmodified.
func (t *Term) WriteRaw(b []byte) {
	if len(b) > 0 {
		t.c.send(request{Op: "raw", Crew: t.key, Data: b})
	}
}

func (t *Term) SetMirror(w io.Writer) {
	t.mu.Lock()
	t.mirror = w
	t.mu.Unlock()
	t.c.call(request{Op: "mirror", Crew: t.key, On: w != nil})
}

func (t *Term) Resize(w, h int) {
	t.mu.Lock()
	same := w == t.w && h == t.h
	t.w, t.h = w, h
	t.mu.Unlock()
	if !same {
		t.c.send(request{Op: "resize", Crew: t.key, W: w, H: h})
	}
}

func (t *Term) Updates() <-chan struct{} { return t.updates }
func (t *Term) Done() <-chan struct{}    { return t.done }

func (t *Term) Exited() bool {
	select {
	case <-t.done:
		return true
	default:
		return false
	}
}

func (t *Term) ExitSummary() string { t.mu.Lock(); defer t.mu.Unlock(); return t.exit }
func (t *Term) Pid() int            { return t.pid }

// Close stops the crew on the server and waits for it to end.
func (t *Term) Close(grace time.Duration) {
	if t.Exited() {
		return
	}
	t.c.send(request{Op: "stop", Crew: t.key, Grace: int(grace / time.Millisecond)})
	select {
	case <-t.done:
	case <-time.After(grace + 5*time.Second):
	}
}

// LastChange is when the crew's screen text last changed, per the server.
func (t *Term) LastChange() time.Time {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.frame.Changed == 0 {
		return time.Now()
	}
	return time.UnixMilli(t.frame.Changed)
}

// Transcript returns a running crew's output so far, as plain text.
func (c *Client) Transcript(key string) (string, error) {
	m, err := c.call(request{Op: "transcript", Crew: key})
	return m.Text, err
}
