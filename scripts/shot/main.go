// Command shot regenerates the terminal screenshots in docs/assets from
// real apic output: it serves the demo API in-process, runs the requests
// the docs talk about, and renders the frames the CLI and the UI produce as
// SVG. Run it with `task shots` after changing anything on screen.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/dataGriff/api-caller/internal/demoapi"
	"github.com/dataGriff/api-caller/internal/httpfile"
	"github.com/dataGriff/api-caller/internal/output"
	"github.com/dataGriff/api-caller/internal/project"
	"github.com/dataGriff/api-caller/internal/runner"
	"github.com/dataGriff/api-caller/internal/session"
	"github.com/dataGriff/api-caller/internal/ui"
)

// demoPort is the port the screenshots show. It only has to be free on the
// machine generating them; the URL in the picture is part of the picture.
const demoPort = 8089

// The size of the terminal in the ui screenshot, in characters.
const (
	uiCols = 112
	uiRows = 19
)

func main() {
	out := flag.String("out", "docs/assets", "directory to write the SVGs into")
	flag.Parse()
	if err := generate(*out); err != nil {
		log.Fatalf("shot: %v", err)
	}
}

func generate(dir string) error {
	// The frames are captured off a terminal, so nothing can detect one:
	// ask lipgloss for colour explicitly, the way a real session has it.
	lipgloss.SetColorProfile(termenv.ANSI256)

	if err := os.MkdirAll(dir, 0o755); err != nil { //nolint:gosec // the docs assets directory the caller named
		return err
	}
	root, stop, err := startDemo()
	if err != nil {
		return err
	}
	defer stop()

	shots := []struct {
		file, title string
		render      func(root string) ([]Frame, error)
	}{
		{"apic-ui.svg", "apic ui --demo", single(uiFrame)},
		{"apic-run.svg", "apic run login whoami", single(runOutput)},
		{"apic-demo.svg", "apic ui --demo", uiAnimation},
	}
	for _, s := range shots {
		frames, err := s.render(root)
		if err != nil {
			return fmt.Errorf("%s: %w", s.file, err)
		}
		path := filepath.Join(dir, s.file)
		if err := os.WriteFile(path, []byte(AnimatedSVG(s.title, frames)), 0o644); err != nil { //nolint:gosec // a committed docs asset
			return err
		}
		fmt.Println("wrote", path)
	}
	return nil
}

// single adapts a one-frame renderer to the animation shape.
func single(render func(root string) (string, error)) func(root string) ([]Frame, error) {
	return func(root string) ([]Frame, error) {
		text, err := render(root)
		if err != nil {
			return nil, err
		}
		return []Frame{{Text: text}}, nil
	}
}

// uiAnimation is the moving picture on the README and the docs home: a
// tour of the UI in about twenty seconds, every frame captured from the
// model after a real key press, so it shows what the UI does today. The
// caption in the title bar names the key.
func uiAnimation(root string) ([]Frame, error) {
	r, err := newRunner(root)
	if err != nil {
		return nil, err
	}
	m := ui.New(ui.Config{Root: "apic-demo", Env: r.Opts.Env, Runner: r, Demo: true, Theme: output.Default()})
	m.Resize(uiCols, uiRows)

	var frames []Frame
	add := func(hold time.Duration, caption string) {
		text := m.View()
		if n := len(frames); n > 0 && frames[n-1].Text == text {
			frames[n-1].Hold += hold // nothing changed on screen: hold the last frame longer
			return
		}
		frames = append(frames, Frame{Text: text, Hold: hold, Caption: caption})
	}

	add(2*time.Second, "the project: requests left, preview right")
	m.Press("enter")
	add(2500*time.Millisecond, "enter · send the request")
	m.Press("j")
	add(1500*time.Millisecond, "j · whoami is ready: login captured the token")
	// Down to the first request in todos.http. The loop is bounded by the
	// project size so a renamed request fails loudly instead of spinning.
	for i := len(r.Project.Requests()); ; i-- {
		if sel := m.Selected(); sel != nil && sel.ID() == "list-todos" {
			break
		}
		if i == 0 {
			return nil, fmt.Errorf("the demo project has no list-todos request to move the cursor to")
		}
		m.Press("j")
	}
	add(time.Second, "j … · todos.http")
	// Run the file as a flow, one frame per request landing.
	m.PressWatch("f", func() { add(450*time.Millisecond, "f · run the whole file, live") })
	frames[len(frames)-1].Hold = 2 * time.Second
	m.Press("3")
	add(2500*time.Millisecond, "3 · checks: actual against expected")
	m.Press("4")
	add(2*time.Second, "4 · session: what was captured")
	m.Press("?")
	add(2500*time.Millisecond, "? · every key")
	m.Press("esc")
	m.Press("1")
	add(1500*time.Millisecond, "esc · back to it")
	return frames, nil
}

// uiFrame drives `apic ui` to the frame the docs lead with: everything run
// once — the demo project has one request that fails on purpose — and the
// cursor on list-todos, showing its response.
func uiFrame(root string) (string, error) {
	r, err := newRunner(root)
	if err != nil {
		return "", err
	}
	m := ui.New(ui.Config{
		// The status bar shows the project root, and a temporary directory
		// would be different in every regeneration; name it the way `apic
		// demo` names the one it scaffolds.
		Root:   "apic-demo",
		Env:    r.Opts.Env,
		Runner: r,
		Demo:   true,
		Theme:  output.Default(),
	})
	m.Resize(uiCols, uiRows)
	// `a` runs everything, `g` goes to the top, then down past auth.http,
	// explore.http and jobs.http to the first request in todos.http.
	m.PressAll("a g j j j j j j j j j j j j")
	if m.Selected() == nil || m.Selected().ID() != "list-todos" {
		return "", fmt.Errorf("the ui frame should show list-todos, got %v", m.Selected())
	}
	return m.View(), nil
}

// runOutput is what `apic run login whoami` prints, under the prompt that
// asked for it.
func runOutput(root string) (string, error) {
	r, err := newRunner(root)
	if err != nil {
		return "", err
	}
	var reqs []*httpfile.Request
	for _, name := range []string{"login", "whoami"} {
		rs, err := r.Project.Resolve(name)
		if err != nil {
			return "", err
		}
		reqs = append(reqs, rs...)
	}
	results, err := r.RunAll(context.Background(), reqs)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	t := output.Default()
	fmt.Fprintf(&b, "%s apic run login whoami\n", t.Accent.Render("$"))
	for i, res := range results {
		if i > 0 {
			fmt.Fprintln(&b)
		}
		output.Human(&b, res, false)
	}
	output.Summary(&b, results)
	return b.String(), nil
}

func newRunner(root string) (*runner.Runner, error) {
	p, err := project.Load(root)
	if err != nil {
		return nil, err
	}
	return runner.New(p, runner.Options{Env: "local", Session: session.NewMemory()})
}

// startDemo writes the demo project to a temporary directory and serves the
// fake API it targets, exactly as `apic ui --demo` does.
func startDemo() (root string, stop func(), err error) {
	dir, err := os.MkdirTemp("", "apic-shot-*")
	if err != nil {
		return "", nil, err
	}
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", demoPort))
	if err != nil {
		_ = os.RemoveAll(dir)
		return "", nil, fmt.Errorf("the screenshots show port %d, which is in use: %w", demoPort, err)
	}
	if _, _, err := demoapi.WriteProject(dir, demoPort, true); err != nil {
		_ = ln.Close()
		_ = os.RemoveAll(dir)
		return "", nil, err
	}
	srv := &http.Server{Handler: demoapi.New(), ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	return dir, func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
		_ = os.RemoveAll(dir)
	}, nil
}
