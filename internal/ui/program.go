package ui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/x/term"
)

// Msg is an event delivered to the model.
type Msg any

// Cmd is work run off the event loop; its Msg comes back to the model.
type Cmd func() Msg

// sizeMsg reports the terminal size.
type sizeMsg struct{ width, height int }

// tickMsg advances the spinner while a run is in flight.
type tickMsg struct{}

// execMsg asks the program to suspend and run a command (the editor).
type execMsg struct {
	cmd  *exec.Cmd
	done func(error) Msg
}

// quitMsg ends the program.
type quitMsg struct{}

// Quit is the command that ends the UI.
func Quit() Msg { return quitMsg{} }

const (
	altScreenOn  = "\x1b[?1049h"
	altScreenOff = "\x1b[?1049l"
	hideCursor   = "\x1b[?25l"
	showCursor   = "\x1b[?25h"
	clearScreen  = "\x1b[2J\x1b[H"
	cursorHome   = "\x1b[H"
	eraseLine    = "\x1b[K"
	eraseBelow   = "\x1b[J"
)

// Run drives the model on a real terminal: raw mode, the alternate screen,
// a size poller and a spinner ticker. It restores the terminal on the way
// out, including when the model panics.
func Run(ctx context.Context, m *Model, in *os.File, out io.Writer) error {
	if !term.IsTerminal(in.Fd()) {
		return errors.New("stdin is not a terminal")
	}
	state, err := term.MakeRaw(in.Fd())
	if err != nil {
		return fmt.Errorf("raw mode: %w (on Windows use Windows Terminal, or winpty in Git Bash)", err)
	}
	p := &program{m: m, in: in, out: out, state: state, msgs: make(chan Msg, 64), done: make(chan struct{})}
	defer p.restore()
	return p.loop(ctx)
}

type program struct {
	m     *Model
	in    *os.File
	out   io.Writer
	state *term.State
	msgs  chan Msg
	done  chan struct{}

	keys     chan Key
	lastSize sizeMsg
	frame    string
	restored bool
}

func (p *program) write(s string) { _, _ = io.WriteString(p.out, s) }

func (p *program) restore() {
	if p.restored {
		return
	}
	p.restored = true
	p.write(showCursor + altScreenOff)
	_ = term.Restore(p.in.Fd(), p.state)
}

func (p *program) loop(ctx context.Context) error {
	p.write(altScreenOn + hideCursor + clearScreen)
	p.keys = make(chan Key, 16)
	go readKeys(p.in, p.keys, p.done)
	defer close(p.done)

	size := time.NewTicker(250 * time.Millisecond)
	defer size.Stop()
	tick := time.NewTicker(110 * time.Millisecond)
	defer tick.Stop()

	p.resize()
	p.render()
	for {
		select {
		case <-ctx.Done():
			return nil
		case k := <-p.keys:
			if p.dispatch(p.m.Update(k)) {
				return nil
			}
		case msg := <-p.msgs:
			if p.dispatch(p.m.Update(msg)) {
				return nil
			}
		case <-size.C:
			if p.resize() {
				p.render()
			}
			continue
		case <-tick.C:
			if !p.m.Running() {
				continue
			}
			if p.dispatch(p.m.Update(tickMsg{})) {
				return nil
			}
		}
		p.render()
	}
}

// dispatch runs a command off the loop and reports whether to quit.
func (p *program) dispatch(cmd Cmd) bool {
	if cmd == nil {
		return false
	}
	switch msg := cmd().(type) {
	case nil:
		return false
	case quitMsg:
		return true
	case execMsg:
		p.exec(msg)
		return false
	case Cmd:
		return p.dispatch(msg)
	case asyncMsg:
		go func() {
			out := msg.run()
			select {
			case p.msgs <- out:
			case <-p.done:
			}
		}()
		return false
	default:
		return p.dispatch(func() Msg { return msg })
	}
}

// exec suspends the UI, runs a command attached to the terminal, and
// restores the UI afterwards.
func (p *program) exec(e execMsg) {
	p.write(showCursor + altScreenOff)
	_ = term.Restore(p.in.Fd(), p.state)
	e.cmd.Stdin, e.cmd.Stdout, e.cmd.Stderr = p.in, os.Stdout, os.Stderr
	err := e.cmd.Run()
	if state, rerr := term.MakeRaw(p.in.Fd()); rerr == nil {
		p.state = state
	}
	p.write(altScreenOn + hideCursor + clearScreen)
	p.frame = ""
	if e.done != nil {
		select {
		case p.msgs <- e.done(err):
		case <-p.done:
		}
	}
}

func (p *program) resize() bool {
	w, h, err := term.GetSize(p.in.Fd())
	if err != nil || w <= 0 || h <= 0 {
		w, h = 80, 24
	}
	if p.lastSize.width == w && p.lastSize.height == h {
		return false
	}
	p.lastSize = sizeMsg{w, h}
	p.m.Update(sizeMsg{w, h})
	p.frame = ""
	return true
}

// render paints the frame. Every line is erased to the end so a shorter
// line cannot leave debris from the previous frame.
func (p *program) render() {
	frame := p.m.View()
	if frame == p.frame {
		return
	}
	p.frame = frame
	var b strings.Builder
	b.WriteString(cursorHome)
	lines := strings.Split(frame, "\n")
	for i, line := range lines {
		b.WriteString(line)
		b.WriteString(eraseLine)
		if i < len(lines)-1 {
			b.WriteString("\r\n")
		}
	}
	b.WriteString(eraseBelow)
	p.write(b.String())
}

// asyncMsg carries work that must not block the event loop.
type asyncMsg struct{ run func() Msg }

// async schedules work off the loop; its result comes back as a message.
func async(run func() Msg) Cmd {
	return func() Msg { return asyncMsg{run: run} }
}

// after schedules a message once d has passed.
func after(d time.Duration, msg Msg) Cmd {
	return async(func() Msg {
		time.Sleep(d)
		return msg
	})
}
