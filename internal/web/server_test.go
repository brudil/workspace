package web

import (
	"bufio"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"
)

var testApp = fstest.MapFS{
	"index.html":    {Data: []byte("<!doctype html><title>app</title>")},
	"assets/app.js": {Data: []byte("console.log('app')")},
}

// fakeWorkspace serves a workspace whose title a test can change.
type fakeWorkspace struct {
	mu    sync.Mutex
	title string
}

func (f *fakeWorkspace) get() (Workspace, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return Workspace{Name: "test", Missions: []Mission{{Slug: "m", Title: f.title}}}, nil
}

func (f *fakeWorkspace) set(title string) {
	f.mu.Lock()
	f.title = title
	f.mu.Unlock()
}

func startTest(t *testing.T, root string, fw *fakeWorkspace, hold func() func()) *Server {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{Root: root, Workspace: fw.get, Hold: hold, Poll: 20 * time.Millisecond, Assets: testApp, listener: ln}
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

func noRedirects() *http.Client {
	return &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func do(t *testing.T, s *Server, method, path string, signedIn bool, edit func(*http.Request)) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(method, s.Info().Origin()+path, strings.NewReader("{}"))
	if signedIn {
		req.AddCookie(&http.Cookie{Name: s.cookieName(), Value: s.Info().Token})
	}
	if edit != nil {
		edit(req)
	}
	res, err := noRedirects().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { res.Body.Close() })
	return res
}

func body(res *http.Response) string {
	data, _ := io.ReadAll(res.Body)
	return string(data)
}

func TestDefaultPort(t *testing.T) {
	a, b := DefaultPort("/Users/me/work"), DefaultPort("/Users/me/work/")
	if a != b || a < portBase || a >= portBase+portRange {
		t.Errorf("ports = %d, %d", a, b)
	}
	if DefaultPort("/Users/me/other") == a {
		t.Log("two workspaces share a port by chance; the next free one is used")
	}
}

// .ws/web.json says where the UI is, readable only by you; the token
// outlives the server, so a signed-in browser stays signed in.
func TestStart_InfoAndToken(t *testing.T) {
	root := t.TempDir()
	s := startTest(t, root, &fakeWorkspace{}, nil)
	info, err := ReadInfo(root)
	if err != nil || info.Port != s.Info().Port || info.Pid != os.Getpid() || len(info.Token) != 64 {
		t.Fatalf("info = %+v, %v", info, err)
	}
	if st, _ := os.Stat(InfoPath(root)); st.Mode().Perm() != 0o600 {
		t.Errorf("web.json mode = %v", st.Mode().Perm())
	}
	s.Close()
	after, _ := ReadInfo(root)
	if after.Port != 0 || after.Pid != 0 || after.Token != info.Token {
		t.Errorf("after close = %+v", after)
	}
	again := startTest(t, root, &fakeWorkspace{}, nil)
	if again.Info().Token != info.Token {
		t.Error("restarting changed the token")
	}
}

// With no [web] port, the workspace's own port, or the next free one; a
// configured port that's taken is an error.
func TestListen_Ports(t *testing.T) {
	root := t.TempDir()
	own := DefaultPort(root)
	blocker, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(own))
	if err != nil {
		t.Skipf("port %d is busy already: %v", own, err)
	}
	defer blocker.Close()
	s := &Server{Root: root, Workspace: (&fakeWorkspace{}).get, Assets: testApp}
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if p := s.Info().Port; p == own || p < portBase || p >= portBase+portRange {
		t.Errorf("port = %d with %d taken", p, own)
	}
	if err := (&Server{Root: t.TempDir(), Port: own, Assets: testApp}).Start(); err == nil || !strings.Contains(err.Error(), "[web] port") {
		t.Errorf("taken [web] port: %v", err)
	}
}

func TestAuth(t *testing.T) {
	s := startTest(t, t.TempDir(), &fakeWorkspace{}, nil)
	if res := do(t, s, "GET", "/auth?t=wrong", false, nil); res.StatusCode != http.StatusForbidden {
		t.Errorf("wrong token: %d", res.StatusCode)
	}
	res := do(t, s, "GET", "/auth?t="+s.Info().Token+"&next=/missions/x", false, nil)
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/missions/x" {
		t.Errorf("sign in: %d to %q", res.StatusCode, res.Header.Get("Location"))
	}
	var cookie *http.Cookie
	for _, c := range res.Cookies() {
		if c.Name == s.cookieName() {
			cookie = c
		}
	}
	if cookie == nil || cookie.Value != s.Info().Token || !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode {
		t.Errorf("cookie = %+v", cookie)
	}
	if res.Header.Get("Referrer-Policy") != "no-referrer" {
		t.Error("the sign-in URL carries the token: no referrer")
	}
	// A sign-in link can't send the browser elsewhere.
	for _, next := range []string{"//evil.example", "https://evil.example", "/\\evil.example", ""} {
		res := do(t, s, "GET", "/auth?t="+s.Info().Token+"&next="+next, false, nil)
		if res.Header.Get("Location") != "/" {
			t.Errorf("next %q went to %q", next, res.Header.Get("Location"))
		}
	}
	// Two workspaces' cookies don't collide (cookies ignore the port).
	other := startTest(t, t.TempDir(), &fakeWorkspace{}, nil)
	if other.cookieName() == s.cookieName() {
		t.Error("two workspaces share a cookie name")
	}
}

func TestGuard(t *testing.T) {
	s := startTest(t, t.TempDir(), &fakeWorkspace{title: "Cache tokens"}, nil)

	res := do(t, s, "GET", "/api/workspace", false, nil)
	if res.StatusCode != http.StatusUnauthorized || !strings.Contains(body(res), "ws web") {
		t.Errorf("signed out API: %d", res.StatusCode)
	}
	if res := do(t, s, "GET", "/missions/m", false, nil); res.StatusCode != http.StatusUnauthorized || strings.Contains(body(res), "<title>app") {
		t.Errorf("signed out page: %d", res.StatusCode)
	}

	res = do(t, s, "GET", "/api/workspace", true, nil)
	var ws Workspace
	if err := json.NewDecoder(res.Body).Decode(&ws); err != nil || res.StatusCode != 200 || ws.Missions[0].Title != "Cache tokens" {
		t.Errorf("signed in: %d %+v %v", res.StatusCode, ws, err)
	}
	bearer := func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+s.Info().Token) }
	if res := do(t, s, "GET", "/api/workspace", false, bearer); res.StatusCode != 200 {
		t.Errorf("bearer: %d", res.StatusCode)
	}

	// DNS rebinding: a name that resolves here isn't us.
	if res := do(t, s, "GET", "/api/workspace", true, func(r *http.Request) { r.Host = "evil.example:" + strconv.Itoa(s.Info().Port) }); res.StatusCode != http.StatusMisdirectedRequest {
		t.Errorf("foreign host: %d", res.StatusCode)
	}
	if res := do(t, s, "GET", "/api/workspace", true, func(r *http.Request) { r.Host = "localhost:" + strconv.Itoa(s.Info().Port) }); res.StatusCode != 200 {
		t.Errorf("localhost: %d", res.StatusCode)
	}

	// Writes need our Origin (another port on 127.0.0.1 is another site's
	// page, and SameSite doesn't stop it) and JSON.
	json := func(origin string) func(*http.Request) {
		return func(r *http.Request) {
			r.Header.Set("Content-Type", "application/json")
			if origin != "" {
				r.Header.Set("Origin", origin)
			}
		}
	}
	if res := do(t, s, "POST", "/api/anything", true, json("")); res.StatusCode != http.StatusForbidden {
		t.Errorf("no origin: %d", res.StatusCode)
	}
	if res := do(t, s, "POST", "/api/anything", true, json("http://127.0.0.1:3000")); res.StatusCode != http.StatusForbidden {
		t.Errorf("another port's page: %d", res.StatusCode)
	}
	if res := do(t, s, "POST", "/api/anything", true, func(r *http.Request) {
		r.Header.Set("Origin", s.Info().Origin())
		r.Header.Set("Content-Type", "text/plain")
	}); res.StatusCode != http.StatusUnsupportedMediaType {
		t.Errorf("a form post: %d", res.StatusCode)
	}
	if res := do(t, s, "POST", "/api/anything", true, json(s.Info().Origin())); res.StatusCode != http.StatusNotFound {
		t.Errorf("an allowed write reaches the API: %d", res.StatusCode)
	}
}

// The app's files are served as they are, hashed assets for good; any
// other path is the app (it routes), but not under /api.
func TestApp(t *testing.T) {
	s := startTest(t, t.TempDir(), &fakeWorkspace{}, nil)
	res := do(t, s, "GET", "/missions/m", true, nil)
	if b := body(res); res.StatusCode != 200 || !strings.Contains(b, "<title>app") || res.Header.Get("Cache-Control") != "no-cache" {
		t.Errorf("app route: %d %q", res.StatusCode, b)
	}
	res = do(t, s, "GET", "/assets/app.js", true, nil)
	if body(res) != "console.log('app')" || !strings.Contains(res.Header.Get("Cache-Control"), "immutable") {
		t.Errorf("asset: %q", res.Header.Get("Cache-Control"))
	}
	if res := do(t, s, "GET", "/api/nope", true, nil); res.StatusCode != http.StatusNotFound || !strings.Contains(body(res), `"error"`) {
		t.Errorf("unknown API: %d", res.StatusCode)
	}
}

// An open page holds the deck server up, and hears when what it shows
// changes.
func TestEvents(t *testing.T) {
	var held atomic.Int32
	hold := func() func() {
		held.Add(1)
		return func() { held.Add(-1) }
	}
	fw := &fakeWorkspace{title: "one"}
	s := startTest(t, t.TempDir(), fw, hold)

	req, _ := http.NewRequest("GET", s.Info().Origin()+"/api/events", nil)
	req.AddCookie(&http.Cookie{Name: s.cookieName(), Value: s.Info().Token})
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if res.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("content type %q", res.Header.Get("Content-Type"))
	}
	events := make(chan string, 10)
	go func() {
		sc := bufio.NewScanner(res.Body)
		for sc.Scan() {
			if name, ok := strings.CutPrefix(sc.Text(), "event: "); ok {
				events <- name
			}
		}
		close(events)
	}()
	next := func() string {
		select {
		case e := <-events:
			return e
		case <-time.After(3 * time.Second):
			return "(nothing)"
		}
	}
	if e := next(); e != "hello" {
		t.Fatalf("first event %q", e)
	}
	if held.Load() != 1 {
		t.Errorf("held %d", held.Load())
	}
	fw.set("two")
	if e := next(); e != "workspace" {
		t.Errorf("after a change: %q", e)
	}
	res.Body.Close()
	deadline := time.Now().Add(3 * time.Second)
	for held.Load() != 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if held.Load() != 0 {
		t.Error("closing the page didn't release the deck server")
	}
}
