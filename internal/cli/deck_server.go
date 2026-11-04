package cli

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/brudil/workspace/internal/deckserver"
	"github.com/brudil/workspace/internal/ui"
	"github.com/spf13/cobra"
)

// EnvDeckServer set to 0 keeps crew inside the deck process (they stop when
// the deck quits) instead of in the workspace's deck server.
const EnvDeckServer = "WS_DECK_SERVER"

func deckServerEnabled() bool { return os.Getenv(EnvDeckServer) != "0" }

// serverHost runs crew in the workspace's deck server. It only starts the
// server when crew are first launched; opening the deck just looks for one
// to reattach to.
type serverHost struct {
	root string

	mu     sync.Mutex
	client *deckserver.Client
}

func (h *serverHost) connect(start bool) (*deckserver.Client, error) {
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
	var c *deckserver.Client
	var err error
	if start {
		c, err = deckserver.Connect(h.root)
	} else {
		c, err = deckserver.Dial(h.root)
	}
	if err != nil {
		return nil, err
	}
	h.client = c
	return c, nil
}

func (h *serverHost) Launch(s crewSpec) (crewTerm, error) {
	c, err := h.connect(true)
	if err != nil {
		return nil, err
	}
	return c.Launch(deckserver.Spec{
		Slug: s.Slug, CrewID: s.CrewID, Harness: s.Harness, Capsule: s.Capsule, Role: s.Role,
		Args: s.Args, Dir: s.Dir, Env: s.Env, Width: s.Width, Height: s.Height,
	})
}

func (h *serverHost) Running() []hostedCrew {
	c, err := h.connect(false)
	if err != nil {
		return nil // no server running: nothing to reattach
	}
	infos, err := c.List()
	if err != nil {
		return nil
	}
	var out []hostedCrew
	for _, info := range infos {
		t, err := c.Attach(info)
		if err != nil {
			continue
		}
		sp := info.Spec
		out = append(out, hostedCrew{
			Spec: crewSpec{Slug: sp.Slug, CrewID: sp.CrewID, Harness: sp.Harness, Capsule: sp.Capsule, Role: sp.Role, Dir: sp.Dir},
			Term: t,
		})
	}
	return out
}

func (h *serverHost) OwnsLifecycle() bool { return false }

func (h *serverHost) Detach() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.client != nil {
		h.client.Close()
		h.client = nil
	}
}

// --- ws deck server ---

func newDeckServerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "server",
		Short: "Manage the workspace's deck server, which keeps crew running between decks",
	}
	cmd.AddCommand(newDeckServerRunCmd(), newDeckServerStatusCmd(), newDeckServerStopCmd())
	return cmd
}

func newDeckServerRunCmd() *cobra.Command {
	var root string
	cmd := &cobra.Command{
		Use:    "run",
		Short:  "Run the deck server in the foreground (ws deck starts it for you)",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if root == "" {
				ctx, err := LoadContext()
				if err != nil {
					return err
				}
				root = ctx.WS.Root
			}
			s := &deckserver.Server{Root: root, Log: log.New(os.Stderr, "deck-server ", log.LstdFlags)}
			if err := s.Listen(); err != nil {
				return err
			}
			sigs := make(chan os.Signal, 1)
			signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
			go func() {
				sig := <-sigs
				s.Shutdown("signal " + sig.String())
			}()
			return s.Serve()
		},
	}
	cmd.Flags().StringVar(&root, "root", "", "workspace root (default: discovered from the current directory)")
	return cmd
}

func newDeckServerStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show whether the deck server is running and which crew it holds",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := LoadContext()
			if err != nil {
				return err
			}
			c, err := deckserver.DialAny(ctx.WS.Root)
			if err != nil {
				fmt.Fprintf(ui.Stderr, "  %s no deck server running\n", ui.Dim.Render("·"))
				return nil
			}
			defer c.Close()
			if !c.Compatible() {
				fmt.Fprintf(ui.Stderr, "  %s a deck server from another ws version is running (pid %d); ws deck server stop stops it\n", ui.Orange.Render("⚠"), c.ServerPid())
				return nil
			}
			crews, err := c.List()
			if err != nil {
				return err
			}
			fmt.Fprintf(ui.Stderr, "  %s deck server running, pid %d (%s)\n", ui.Green.Render("●"), c.ServerPid(), deckserver.SocketPath(ctx.WS.Root))
			if len(crews) == 0 {
				fmt.Fprintf(ui.Stderr, "    %s no crew; it exits after a minute idle\n", ui.Dim.Render("·"))
			}
			for _, cr := range crews {
				fmt.Fprintf(ui.Stderr, "    %s %s %s in %s %s\n", ui.Green.Render("●"+cr.Spec.CrewID), cr.Spec.Slug, cr.Spec.Harness, cr.Spec.Capsule, ui.Dim.Render(fmt.Sprintf("pid %d", cr.Pid)))
			}
			return nil
		},
	}
}

func newDeckServerStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop every crew the deck server holds, and the server",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := LoadContext()
			if err != nil {
				return err
			}
			c, err := deckserver.DialAny(ctx.WS.Root)
			if err != nil {
				if !deckserver.IsNoServer(err) {
					return err
				}
				fmt.Fprintf(ui.Stderr, "  %s no deck server running\n", ui.Dim.Render("·"))
				return nil
			}
			defer c.Close()
			var crews []deckserver.CrewInfo
			if c.Compatible() {
				crews, _ = c.List()
			}
			if err := c.Shutdown(); err != nil {
				return err
			}
			<-c.Lost()
			fmt.Fprintf(ui.Stderr, "  %s deck server stopped (%d crew hung up)\n", ui.Green.Render("✓"), len(crews))
			return nil
		},
	}
}
