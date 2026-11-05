package deckserver

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/brudil/workspace/internal/crew"
	"github.com/brudil/workspace/internal/mission"
	"github.com/brudil/workspace/internal/termpane"
)

// frameInterval caps how often a deck gets frames for one crew.
const frameInterval = 16 * time.Millisecond

// Server owns crew for one workspace.
type Server struct {
	Root string
	// IdleTimeout is how long the server stays up with no crew and no decks.
	IdleTimeout time.Duration
	Log         *log.Logger
	// OnCrewEnded, if set, runs after a crew's end is recorded (see
	// crew.EndHook).
	OnCrewEnded crew.EndHook

	store mission.Store
	ln    net.Listener

	mu      sync.Mutex
	crews   map[string]*crewProc
	conns   map[*conn]bool
	idle    *time.Timer
	closing bool
	done    chan struct{}

	watchers sync.WaitGroup // crew end recorders; Shutdown waits for them
}

type crewProc struct {
	spec Spec
	pane *termpane.Pane

	stopRequested atomic.Bool

	mu   sync.Mutex
	subs map[*conn]*sub
}

// sub is one deck attached to one crew.
type sub struct {
	c      *conn
	wake   chan struct{}
	offset int
	mirror bool
	quit   chan struct{}
}

type conn struct {
	nc  net.Conn
	wmu sync.Mutex
	enc *json.Encoder
}

func (c *conn) send(m message) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	c.nc.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return c.enc.Encode(m)
}

// Listen binds the workspace socket. It fails if another server is live.
func (s *Server) Listen() error {
	path := SocketPath(s.Root)
	if c, err := net.DialTimeout("unix", path, time.Second); err == nil {
		c.Close()
		return fmt.Errorf("a deck server is already listening on %s", path)
	}
	os.Remove(path) // stale socket from a server that died
	os.MkdirAll(dirOf(path), 0o755)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return err
	}
	os.Chmod(path, 0o600)
	s.ln = ln
	s.store = mission.Store{Root: s.Root}
	s.crews = map[string]*crewProc{}
	s.conns = map[*conn]bool{}
	s.done = make(chan struct{})
	if s.Log == nil {
		s.Log = log.New(io.Discard, "", 0)
	}
	if s.IdleTimeout == 0 {
		s.IdleTimeout = time.Minute
	}
	s.mu.Lock()
	s.checkIdleLocked()
	s.mu.Unlock()
	return nil
}

func dirOf(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' {
			return p[:i]
		}
	}
	return "."
}

// Serve accepts decks until Shutdown or the idle timeout.
func (s *Server) Serve() error {
	s.Log.Printf("deck server for %s listening on %s (pid %d)", s.Root, SocketPath(s.Root), os.Getpid())
	for {
		nc, err := s.ln.Accept()
		if err != nil {
			s.mu.Lock()
			closing := s.closing
			s.mu.Unlock()
			if closing && errors.Is(err, net.ErrClosed) {
				<-s.done // Shutdown has removed the socket and finished
				return nil
			}
			return err
		}
		c := &conn{nc: nc, enc: json.NewEncoder(nc)}
		s.mu.Lock()
		s.conns[c] = true
		s.checkIdleLocked()
		s.mu.Unlock()
		go s.handle(c)
	}
}

// Shutdown stops every crew (recording their ends) and closes the socket.
func (s *Server) Shutdown(reason string) {
	s.mu.Lock()
	if s.closing {
		s.mu.Unlock()
		return
	}
	s.closing = true
	crews := make([]*crewProc, 0, len(s.crews))
	for _, cr := range s.crews {
		crews = append(crews, cr)
	}
	s.mu.Unlock()

	var wg sync.WaitGroup
	for _, cr := range crews {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cr.pane.Close(3 * time.Second)
		}()
	}
	wg.Wait()
	s.watchers.Wait() // every crew's end is on its mission before we exit
	s.Log.Printf("shutting down: %s", reason)
	s.ln.Close()
	os.Remove(SocketPath(s.Root))
	close(s.done) // last: whoever waits on Done finds the socket gone
}

// Done is closed once the server has shut down and removed its socket.
func (s *Server) Done() <-chan struct{} { return s.done }

// checkIdleLocked arms the idle timer when nothing is running or attached.
func (s *Server) checkIdleLocked() {
	if s.idle != nil {
		s.idle.Stop()
		s.idle = nil
	}
	if len(s.crews) > 0 || len(s.conns) > 0 || s.closing {
		return
	}
	s.idle = time.AfterFunc(s.IdleTimeout, func() {
		s.mu.Lock()
		busy := len(s.crews) > 0 || len(s.conns) > 0
		s.mu.Unlock()
		if !busy {
			go s.Shutdown("idle")
		}
	})
}

func (s *Server) handle(c *conn) {
	defer func() {
		c.nc.Close()
		s.mu.Lock()
		delete(s.conns, c)
		for _, cr := range s.crews {
			cr.detach(c)
		}
		s.checkIdleLocked()
		s.mu.Unlock()
	}()
	sc := bufio.NewScanner(c.nc)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		var req request
		if err := json.Unmarshal(sc.Bytes(), &req); err != nil {
			continue
		}
		reply := s.do(c, req)
		reply.ID = req.ID
		if err := c.send(reply); err != nil {
			return
		}
	}
}

func (s *Server) crew(key string) (*crewProc, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cr, ok := s.crews[key]
	if !ok {
		return nil, fmt.Errorf("no running crew %s", key)
	}
	return cr, nil
}

func (s *Server) do(c *conn, req request) message {
	fail := func(err error) message { return message{Error: err.Error()} }
	switch req.Op {
	case "hello":
		return message{Ver: ProtocolVersion, Pid: os.Getpid()}

	case "list":
		s.mu.Lock()
		defer s.mu.Unlock()
		var out []CrewInfo
		for _, cr := range s.crews {
			out = append(out, CrewInfo{Spec: cr.spec, Pid: cr.pane.Pid()})
		}
		return message{Crews: out}

	case "launch":
		if req.Spec == nil || len(req.Spec.Args) == 0 {
			return fail(errors.New("launch needs a spec with args"))
		}
		pid, err := s.launch(*req.Spec)
		if err != nil {
			return fail(err)
		}
		return message{Pid: pid}

	case "shutdown":
		go s.Shutdown("requested")
		return message{}
	}

	cr, err := s.crew(req.Crew)
	if err != nil {
		return fail(err)
	}
	switch req.Op {
	case "transcript":
		return message{Text: cr.pane.Transcript()}
	case "attach":
		cr.attach(c)
	case "detach":
		cr.detach(c)
	case "key":
		if req.Key != nil {
			cr.pane.SendKey(*req.Key)
		}
	case "mouse":
		if req.Mouse != nil {
			cr.pane.SendMouse(uvWheel(*req.Mouse))
		}
	case "paste":
		cr.pane.Paste(req.Text)
	case "raw":
		cr.pane.WriteRaw(req.Data)
	case "resize":
		cr.pane.Resize(req.W, req.H)
	case "scroll":
		cr.setOffset(c, req.Offset)
	case "mirror":
		cr.setMirror(c, req.On)
	case "stop":
		s.Log.Printf("stop requested for %s", req.Crew)
		grace := time.Duration(req.Grace) * time.Millisecond
		if grace <= 0 {
			grace = 3 * time.Second
		}
		// Hang up in the background: this connection may carry input for
		// other crew. The deck learns of the end from the exit event.
		cr.stopRequested.Store(true)
		go cr.pane.Close(grace)
	default:
		return fail(fmt.Errorf("unknown op %q", req.Op))
	}
	return message{}
}

func (s *Server) launch(spec Spec) (int, error) {
	s.mu.Lock()
	if s.closing {
		s.mu.Unlock()
		return 0, errors.New("server is shutting down")
	}
	if _, dup := s.crews[spec.Key()]; dup {
		s.mu.Unlock()
		return 0, fmt.Errorf("crew %s is already running", spec.Key())
	}
	s.mu.Unlock()

	cmd := exec.Command(spec.Args[0], spec.Args[1:]...)
	cmd.Dir = spec.Dir
	cmd.Env = spec.Env
	pane, err := termpane.Start(cmd, spec.Width, spec.Height)
	if err != nil {
		return 0, err
	}
	cr := &crewProc{spec: spec, pane: pane, subs: map[*conn]*sub{}}

	s.mu.Lock()
	s.crews[spec.Key()] = cr
	s.checkIdleLocked()
	s.mu.Unlock()
	s.Log.Printf("launched %s (%s) pid %d in %s: %s", spec.Key(), spec.Harness, pane.Pid(), spec.Dir, argsForLog(spec.Args))

	go cr.fanOut()
	s.watchers.Add(1)
	go func() {
		defer s.watchers.Done()
		s.watch(cr)
	}()
	return pane.Pid(), nil
}

// watch records the crew's end on its mission and tells attached decks.
func (s *Server) watch(cr *crewProc) {
	<-cr.pane.Done()
	exit := cr.pane.ExitSummary()
	s.mu.Lock()
	closing := s.closing
	s.mu.Unlock()
	if closing {
		exit = "stopped: " + exit
	}
	s.Log.Printf("%s ended: %s", cr.spec.Key(), exit)
	if err := crew.Finish(s.store, cr.spec, exit, cr.pane.Transcript(), closing || cr.stopRequested.Load(), s.OnCrewEnded); err != nil {
		s.Log.Printf("recording end of %s: %v", cr.spec.Key(), err)
	}

	cr.mu.Lock()
	for c, sb := range cr.subs {
		close(sb.quit)
		c.send(message{Ev: "exit", Crew: cr.spec.Key(), Exit: exit})
	}
	cr.subs = map[*conn]*sub{}
	cr.mu.Unlock()

	s.mu.Lock()
	delete(s.crews, cr.spec.Key())
	s.checkIdleLocked()
	s.mu.Unlock()
}

// fanOut turns the pane's single update signal into one per attached deck.
func (cr *crewProc) fanOut() {
	for {
		select {
		case <-cr.pane.Updates():
		case <-cr.pane.Done():
			return
		}
		cr.mu.Lock()
		for _, sb := range cr.subs {
			select {
			case sb.wake <- struct{}{}:
			default:
			}
		}
		cr.mu.Unlock()
	}
}

func (cr *crewProc) attach(c *conn) {
	cr.mu.Lock()
	defer cr.mu.Unlock()
	if _, ok := cr.subs[c]; ok {
		return
	}
	sb := &sub{c: c, wake: make(chan struct{}, 1), quit: make(chan struct{})}
	cr.subs[c] = sb
	sb.wake <- struct{}{} // first frame straight away
	go cr.push(sb)
}

func (cr *crewProc) detach(c *conn) {
	cr.mu.Lock()
	sb, ok := cr.subs[c]
	if ok {
		delete(cr.subs, c)
		close(sb.quit)
	}
	cr.mu.Unlock()
	if ok && sb.mirror {
		cr.refreshMirror()
	}
}

// push sends frames to one deck, at most one per frameInterval.
func (cr *crewProc) push(sb *sub) {
	for {
		select {
		case <-sb.quit:
			return
		case <-sb.wake:
		}
		cr.mu.Lock()
		offset := sb.offset
		cr.mu.Unlock()
		x, y, visible := cr.pane.Cursor()
		w, h := cr.pane.Size()
		f := &Frame{
			Screen:        cr.pane.RenderScrolled(offset),
			Offset:        offset,
			ScrollbackLen: cr.pane.ScrollbackLen(),
			AltScreen:     cr.pane.AltScreen(),
			CursorX:       x, CursorY: y, CursorVisible: visible,
			Title: cr.pane.Title(),
			Width: w, Height: h,
			Changed: cr.pane.LastChange().UnixMilli(),
		}
		if err := sb.c.send(message{Ev: "frame", Crew: cr.spec.Key(), Frame: f}); err != nil {
			return
		}
		time.Sleep(frameInterval)
	}
}

func (cr *crewProc) setOffset(c *conn, offset int) {
	cr.mu.Lock()
	defer cr.mu.Unlock()
	if sb, ok := cr.subs[c]; ok {
		sb.offset = max(offset, 0)
		select {
		case sb.wake <- struct{}{}:
		default:
		}
	}
}

func (cr *crewProc) setMirror(c *conn, on bool) {
	cr.mu.Lock()
	if sb, ok := cr.subs[c]; ok {
		sb.mirror = on
	}
	cr.mu.Unlock()
	cr.refreshMirror()
}

// refreshMirror points the pane's raw mirror at every deck that wants it.
func (cr *crewProc) refreshMirror() {
	cr.mu.Lock()
	var want []*conn
	for c, sb := range cr.subs {
		if sb.mirror {
			want = append(want, c)
		}
	}
	cr.mu.Unlock()
	if len(want) == 0 {
		cr.pane.SetMirror(nil)
		return
	}
	key := cr.spec.Key()
	cr.pane.SetMirror(writerFunc(func(p []byte) (int, error) {
		data := append([]byte(nil), p...)
		for _, c := range want {
			c.send(message{Ev: "raw", Crew: key, Data: data})
		}
		return len(p), nil
	}))
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

// argsForLog shows a command line with long arguments shortened.
func argsForLog(args []string) string {
	out := make([]string, len(args))
	for i, a := range args {
		a = strings.ReplaceAll(a, "\n", "⏎")
		if len(a) > 60 {
			a = a[:57] + "…"
		}
		out[i] = a
	}
	return strings.Join(out, " ")
}
