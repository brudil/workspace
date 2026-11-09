// Package web is the deck's web UI: an HTTP server on 127.0.0.1, run inside
// the deck server, serving a single-page app (web/, built into dist/) and
// the JSON API it reads. It's for what the terminal is poor at, starting
// with review.
//
// Every request needs the workspace's token (a cookie set by /auth, or a
// bearer header), a Host naming this server (against DNS rebinding), and,
// for anything but a read, a matching Origin and a JSON body (against
// cross-site requests, including from other ports on 127.0.0.1).
package web

import (
	"context"
	"crypto/subtle"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"io/fs"
	"log"
	"mime"
	"net"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"syscall"
	"time"
)

//go:embed all:dist
var embedded embed.FS

// App is the built single-page app.
func App() fs.FS {
	sub, err := fs.Sub(embedded, "dist")
	if err != nil {
		panic(err) // dist is embedded at build time
	}
	return sub
}

// Server serves one workspace's web UI.
type Server struct {
	Root string
	// Port is [web] port; 0 picks the workspace's own (DefaultPort), or one
	// of the next few if that's taken.
	Port int
	Log  *log.Logger
	// Workspace is what GET /api/workspace serves.
	Workspace func() (Workspace, error)
	// Capsule finds a mission's capsule to review: its folder, and the ref
	// its changes are compared with. ErrNotFound if the mission has no
	// such capsule (review is only of mission capsules).
	Capsule func(slug, repo, name string) (dir, baseRef string, err error)
	// Hold keeps the deck server up while a page is open (each holds an
	// event stream); nil for nothing to hold.
	Hold func() (release func())
	// Poll is how often open pages are checked for changes to tell them
	// about (default 2s).
	Poll time.Duration
	// Assets is the app to serve (default App()).
	Assets fs.FS

	listener net.Listener // tests set it
	info     Info
	srv      *http.Server
	hub      *hub
	ctx      context.Context
	cancel   context.CancelFunc
}

// Start binds, writes .ws/web.json and serves in the background.
func (s *Server) Start() error {
	if s.Log == nil {
		s.Log = log.New(io.Discard, "", 0)
	}
	if s.Poll == 0 {
		s.Poll = 2 * time.Second
	}
	if s.Assets == nil {
		s.Assets = App()
	}
	ln := s.listener
	if ln == nil {
		var err error
		if ln, err = s.listen(); err != nil {
			return err
		}
	}
	prev, err := ReadInfo(s.Root)
	if err != nil {
		s.Log.Printf("web: %v; making a new token", err)
	}
	token := prev.Token
	if token == "" {
		if token, err = newToken(); err != nil {
			ln.Close()
			return err
		}
	}
	s.info = Info{Port: ln.Addr().(*net.TCPAddr).Port, Pid: os.Getpid(), Token: token}
	if err := writeInfo(s.Root, s.info); err != nil {
		ln.Close()
		return fmt.Errorf("writing %s: %w", InfoPath(s.Root), err)
	}
	s.ctx, s.cancel = context.WithCancel(context.Background())
	s.hub = newHub(s.Poll, s.fingerprint)
	go s.hub.run(s.ctx)
	s.srv = &http.Server{
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return s.ctx },
	}
	go func() {
		if err := s.srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.Log.Printf("web: %v", err)
		}
	}()
	s.Log.Printf("web UI on %s", s.info.Origin())
	return nil
}

// listen binds [web] port, or the workspace's own port or one of the next
// few.
func (s *Server) listen() (net.Listener, error) {
	if s.Port != 0 {
		ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(s.Port)))
		if err != nil {
			return nil, fmt.Errorf("[web] port %d: %w", s.Port, err)
		}
		return ln, nil
	}
	first := DefaultPort(s.Root)
	var lastErr error
	for i := range portTries {
		port := portBase + (first-portBase+i)%portRange
		ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
		if err == nil {
			return ln, nil
		}
		lastErr = err
		if !errors.Is(err, syscall.EADDRINUSE) {
			break
		}
	}
	return nil, fmt.Errorf("no free port from %d (set [web] port): %w", first, lastErr)
}

// Info is where the server listens, and its token.
func (s *Server) Info() Info { return s.info }

// Close stops serving, ends open event streams, and marks .ws/web.json as
// not running (keeping the token).
func (s *Server) Close() {
	if s.srv == nil {
		return
	}
	s.cancel()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := s.srv.Shutdown(ctx); err != nil {
		s.srv.Close()
	}
	if cur, err := ReadInfo(s.Root); err == nil && cur.Pid == s.info.Pid {
		writeInfo(s.Root, Info{Token: s.info.Token})
	}
}

// cookieName is per workspace: cookies are kept per host, not per port, so
// two workspaces' UIs on 127.0.0.1 would otherwise share one.
func (s *Server) cookieName() string {
	h := fnv.New32a()
	h.Write([]byte(s.Root))
	return fmt.Sprintf("ws_%08x", h.Sum32())
}

// Handler is the whole web UI behind its guard.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /auth", s.auth)
	mux.HandleFunc("GET /api/workspace", s.workspace)
	mux.HandleFunc("GET /api/events", s.events)
	mux.HandleFunc("GET /api/missions/{slug}/capsules/{repo}/{name}/diff", s.diff)
	mux.HandleFunc("GET /api/missions/{slug}/capsules/{repo}/{name}/file-diff", s.fileDiff)
	mux.HandleFunc("GET /api/missions/{slug}/capsules/{repo}/{name}/blob", s.blob)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		jsonError(w, http.StatusNotFound, "no such API: "+r.Method+" "+r.URL.Path)
	})
	mux.HandleFunc("/", s.app)
	return s.guard(mux)
}

func (s *Server) origins() []string {
	p := strconv.Itoa(s.info.Port)
	return []string{"http://127.0.0.1:" + p, "http://localhost:" + p}
}

func (s *Server) hostAllowed(host string) bool {
	p := strconv.Itoa(s.info.Port)
	return host == "127.0.0.1:"+p || host == "localhost:"+p
}

func (s *Server) signedIn(r *http.Request) bool {
	given := ""
	if c, err := r.Cookie(s.cookieName()); err == nil {
		given = c.Value
	} else if b, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
		given = b
	}
	return given != "" && subtle.ConstantTimeCompare([]byte(given), []byte(s.info.Token)) == 1
}

func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer") // /auth's URL carries the token
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; frame-ancestors 'none'")
		if !s.hostAllowed(r.Host) {
			http.Error(w, "this server answers only to 127.0.0.1 and localhost", http.StatusMisdirectedRequest)
			return
		}
		if r.URL.Path == "/auth" {
			next.ServeHTTP(w, r)
			return
		}
		if !s.signedIn(r) {
			if strings.HasPrefix(r.URL.Path, "/api/") {
				jsonError(w, http.StatusUnauthorized, "not signed in: open the web UI with `ws web`")
				return
			}
			page(w, http.StatusUnauthorized, "Not signed in", "Open this workspace's web UI from your terminal with <code>ws web</code>, or <code>o</code> in <code>ws deck</code>.")
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			origin := r.Header.Get("Origin")
			ok := false
			for _, o := range s.origins() {
				ok = ok || origin == o
			}
			if !ok {
				jsonError(w, http.StatusForbidden, "cross-origin request refused")
				return
			}
			if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt != "application/json" {
				jsonError(w, http.StatusUnsupportedMediaType, "send JSON")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// auth signs a browser in with the token from ws web's link, then sends it
// on to where the link was for.
func (s *Server) auth(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if subtle.ConstantTimeCompare([]byte(q.Get("t")), []byte(s.info.Token)) != 1 {
		page(w, http.StatusForbidden, "That link has expired", "Open the web UI again with <code>ws web</code>.")
		return
	}
	// Lax, not Strict: a link to the UI from elsewhere (a notification, a
	// chat) still opens signed in. Reads are all a cross-site request can
	// make, and it can't see their answers; writes need our Origin.
	http.SetCookie(w, &http.Cookie{
		Name: s.cookieName(), Value: s.info.Token, Path: "/",
		HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 400 * 24 * 60 * 60,
	})
	http.Redirect(w, r, safeNext(q.Get("next")), http.StatusSeeOther)
}

func (s *Server) workspace(w http.ResponseWriter, r *http.Request) {
	ws, err := s.Workspace()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, ws)
}

// fingerprint says whether what the API serves has changed.
func (s *Server) fingerprint() string {
	ws, err := s.Workspace()
	if err != nil {
		return "error: " + err.Error()
	}
	data, _ := json.Marshal(ws)
	h := fnv.New64a()
	h.Write(data)
	return strconv.FormatUint(h.Sum64(), 16)
}

// events is the page's event stream: it says when something it shows has
// changed, so the page never polls. An open stream holds the deck server up.
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		jsonError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	if s.Hold != nil {
		defer s.Hold()()
	}
	ch, done := s.hub.subscribe()
	defer done()
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	fmt.Fprint(w, "retry: 2000\nevent: hello\ndata: {}\n\n")
	flusher.Flush()
	ping := time.NewTicker(20 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case name := <-ch:
			fmt.Fprintf(w, "event: %s\ndata: {}\n\n", name)
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
		}
		flusher.Flush()
	}
}

// app serves the single-page app: its files, and index.html for any other
// path (the app routes).
func (s *Server) app(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if name != "" && name != "index.html" {
		if info, err := fs.Stat(s.Assets, name); err == nil && !info.IsDir() {
			if strings.HasPrefix(name, "assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable") // hashed names
			}
			http.ServeFileFS(w, r, s.Assets, name)
			return
		}
	}
	index, err := fs.ReadFile(s.Assets, "index.html")
	if err != nil {
		page(w, http.StatusInternalServerError, "No web UI built", "This ws was built without its web UI: run <code>just build</code>.")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(index)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func jsonError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// page is a minimal HTML page for when the app can't be served.
func page(w http.ResponseWriter, status int, title, body string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	fmt.Fprintf(w, `<!doctype html><meta charset="utf-8"><title>%s · ws</title>
<style>body{font:15px/1.5 system-ui,sans-serif;max-width:32rem;margin:20vh auto;padding:0 1rem;color:#333}code{background:#eee;padding:.1em .3em;border-radius:3px}@media(prefers-color-scheme:dark){body{background:#111;color:#ddd}code{background:#222}}</style>
<h1>%s</h1><p>%s</p>`, title, title, body)
}
