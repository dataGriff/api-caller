package ui

import "strings"

// This file drives a model without a terminal or an event loop: it feeds
// keys in and resolves the work they schedule inline. Tests use it, and so
// does the docs screenshot generator, which is why the screenshots show
// what the UI really draws rather than a hand-written approximation.

// settleLimit bounds a command chain so a bug cannot hang the caller. A
// flow over the whole project is the longest legitimate chain, and it is
// two commands per request.
const settleLimit = 1000

// ParseKey turns a binding name — "j", "enter", "ctrl+d", "shift+tab" — into
// a keypress. Anything else is taken as the literal rune, so "?" and "/"
// work as written.
func ParseKey(name string) Key {
	for _, t := range []KeyType{KeyEnter, KeyEsc, KeyTab, KeyShiftTab, KeyBackspace, KeyUp, KeyDown,
		KeyLeft, KeyRight, KeyHome, KeyEnd, KeyPgUp, KeyPgDown, KeyCtrlC, KeyCtrlD, KeyCtrlU} {
		if (Key{Type: t}).String() == name {
			return Key{Type: t}
		}
	}
	r := []rune(name)
	if len(r) == 0 {
		return Key{}
	}
	return runeKey(r[0])
}

// Resize tells the model how big the terminal is. The program loop does
// this from the real terminal; callers without one say so themselves.
func (m *Model) Resize(width, height int) { m.Update(sizeMsg{width: width, height: height}) }

// Press feeds one key and settles everything it starts, so a run sent by
// enter has landed by the time Press returns.
func (m *Model) Press(name string) { m.settle(m.Update(ParseKey(name))) }

// PressAll presses several keys in order, splitting a space-separated list
// for brevity: "j enter 3".
func (m *Model) PressAll(keys string) {
	for _, k := range strings.Fields(keys) {
		m.Press(k)
	}
}

// settle resolves a command chain inline. Work the event loop would do in
// the background — a request, the next step of a flow — runs here and its
// message goes straight back to the model. Anything that needs the real
// program (a timer, the editor, quitting) ends the chain instead.
func (m *Model) settle(cmd Cmd) {
	for i := 0; cmd != nil && i < settleLimit; i++ {
		switch msg := cmd().(type) {
		case nil, quitMsg, delayMsg, execMsg:
			return
		case asyncMsg:
			cmd = m.Update(msg.run())
		case Cmd:
			cmd = msg
		default:
			cmd = m.Update(msg)
		}
	}
}
