package ui

import "slices"

// binding is a set of key names with a help entry.
type binding struct {
	keys []string
	name string // shown in help, e.g. "↑/k"
	help string
}

func bind(name, help string, keys ...string) binding {
	return binding{keys: keys, name: name, help: help}
}

// matches reports whether k triggers the binding.
func (b binding) matches(k Key) bool { return slices.Contains(b.keys, k.String()) }

type keyMap struct {
	Up, Down, Top, Bottom binding
	Run, RunFile, RunAll  binding
	Filter                binding
	NextTab, PrevTab      binding
	Tab1, Tab2, Tab3      binding
	Tab4                  binding
	Headers, Curl         binding
	Open, Env, Reload     binding
	Clear                 binding
	PageUp, PageDown      binding
	Esc, Help, Quit       binding
}

func newKeyMap() keyMap {
	return keyMap{
		Up:       bind("↑/k", "up", "k", "up"),
		Down:     bind("↓/j", "down", "j", "down"),
		Top:      bind("g", "first request", "g", "home"),
		Bottom:   bind("G", "last request", "G", "end"),
		Run:      bind("enter", "run selected", "enter"),
		RunFile:  bind("f", "run the file as a flow", "f"),
		RunAll:   bind("a", "run everything", "a"),
		Filter:   bind("/", "filter requests", "/"),
		NextTab:  bind("tab/l", "next tab", "tab", "l", "right"),
		PrevTab:  bind("shift+tab/h", "previous tab", "shift+tab", "h", "left"),
		Tab1:     bind("1", "preview tab", "1"),
		Tab2:     bind("2", "response tab", "2"),
		Tab3:     bind("3", "checks tab", "3"),
		Tab4:     bind("4", "session tab", "4"),
		Headers:  bind("H", "toggle headers", "H"),
		Curl:     bind("c", "toggle the curl command", "c"),
		Open:     bind("o", "open in $EDITOR", "o"),
		Env:      bind("e", "next environment", "e"),
		Reload:   bind("r", "reload the project", "r"),
		Clear:    bind("x", "clear the session", "x"),
		PageUp:   bind("pgup/ctrl+u", "scroll up", "pgup", "ctrl+u"),
		PageDown: bind("pgdn/ctrl+d", "scroll down", "pgdown", "ctrl+d"),
		Esc:      bind("esc", "cancel, or go back", "esc"),
		Help:     bind("?", "this help", "?"),
		Quit:     bind("q", "quit", "q", "ctrl+c"),
	}
}

// columns groups the bindings for the help overlay.
func (k keyMap) columns() [][]binding {
	return [][]binding{
		{k.Up, k.Down, k.Top, k.Bottom, k.Filter, k.PageUp, k.PageDown},
		{k.Run, k.RunFile, k.RunAll, k.Esc, k.Env, k.Reload, k.Open},
		{k.NextTab, k.PrevTab, k.Tab1, k.Tab2, k.Tab3, k.Tab4},
		{k.Headers, k.Curl, k.Clear, k.Help, k.Quit},
	}
}
