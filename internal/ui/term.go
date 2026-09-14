package ui

import (
	"io"
	"time"
	"unicode/utf8"
)

// KeyType names the keys the UI reacts to. Anything printable arrives as
// KeyRune with the rune set.
type KeyType int

// Key types.
const (
	KeyRune KeyType = iota
	KeyEnter
	KeyEsc
	KeyTab
	KeyShiftTab
	KeyBackspace
	KeyUp
	KeyDown
	KeyLeft
	KeyRight
	KeyHome
	KeyEnd
	KeyPgUp
	KeyPgDown
	KeyCtrlC
	KeyCtrlD
	KeyCtrlU
)

// Key is one decoded keypress.
type Key struct {
	Type KeyType
	Rune rune
}

// String is the name used in key bindings: "j", "enter", "ctrl+c", "up".
func (k Key) String() string {
	switch k.Type {
	case KeyRune:
		return string(k.Rune)
	case KeyEnter:
		return "enter"
	case KeyEsc:
		return "esc"
	case KeyTab:
		return "tab"
	case KeyShiftTab:
		return "shift+tab"
	case KeyBackspace:
		return "backspace"
	case KeyUp:
		return "up"
	case KeyDown:
		return "down"
	case KeyLeft:
		return "left"
	case KeyRight:
		return "right"
	case KeyHome:
		return "home"
	case KeyEnd:
		return "end"
	case KeyPgUp:
		return "pgup"
	case KeyPgDown:
		return "pgdown"
	case KeyCtrlC:
		return "ctrl+c"
	case KeyCtrlD:
		return "ctrl+d"
	case KeyCtrlU:
		return "ctrl+u"
	}
	return ""
}

// rune builds a printable keypress, for tests and bindings.
func runeKey(r rune) Key { return Key{Type: KeyRune, Rune: r} }

// decodeKey decodes the first key in b. It returns the key, how many bytes
// it consumed, and false when b holds no complete key.
func decodeKey(b []byte) (Key, int, bool) {
	if len(b) == 0 {
		return Key{}, 0, false
	}
	switch b[0] {
	case 0x03:
		return Key{Type: KeyCtrlC}, 1, true
	case 0x04:
		return Key{Type: KeyCtrlD}, 1, true
	case 0x15:
		return Key{Type: KeyCtrlU}, 1, true
	case '\r', '\n':
		return Key{Type: KeyEnter}, 1, true
	case '\t':
		return Key{Type: KeyTab}, 1, true
	case 0x08, 0x7f:
		return Key{Type: KeyBackspace}, 1, true
	case 0x1b:
		return decodeEscape(b)
	}
	if b[0] < 0x20 {
		// An unhandled control character: skip it rather than showing a glyph.
		return Key{}, 1, false
	}
	r, size := utf8.DecodeRune(b)
	if r == utf8.RuneError && size <= 1 {
		if len(b) < 4 {
			return Key{}, 0, false // a multi-byte rune split across reads
		}
		return Key{}, 1, false
	}
	return runeKey(r), size, true
}

// decodeEscape decodes an escape sequence. A sequence that is not complete
// yet consumes nothing: readKeys waits briefly for the rest, and treats a
// lone ESC that never grows as the escape key.
func decodeEscape(b []byte) (Key, int, bool) {
	if len(b) == 1 {
		return Key{}, 0, false
	}
	switch b[1] {
	case '[', 'O':
		if len(b) < 3 {
			return Key{}, 0, false
		}
		switch b[2] {
		case 'A':
			return Key{Type: KeyUp}, 3, true
		case 'B':
			return Key{Type: KeyDown}, 3, true
		case 'C':
			return Key{Type: KeyRight}, 3, true
		case 'D':
			return Key{Type: KeyLeft}, 3, true
		case 'H':
			return Key{Type: KeyHome}, 3, true
		case 'F':
			return Key{Type: KeyEnd}, 3, true
		case 'Z':
			return Key{Type: KeyShiftTab}, 3, true
		}
		// CSI <number> ~ and mouse reports, which the UI ignores.
		for i := 2; i < len(b); i++ {
			if b[i] >= 0x40 && b[i] <= 0x7e {
				if b[i] == '~' {
					switch string(b[2:i]) {
					case "1", "7":
						return Key{Type: KeyHome}, i + 1, true
					case "4", "8":
						return Key{Type: KeyEnd}, i + 1, true
					case "5":
						return Key{Type: KeyPgUp}, i + 1, true
					case "6":
						return Key{Type: KeyPgDown}, i + 1, true
					}
				}
				return Key{}, i + 1, false
			}
		}
		return Key{}, 0, false // incomplete sequence
	}
	// ESC followed by something else: treat the ESC alone.
	return Key{Type: KeyEsc}, 1, true
}

// escapeGrace is how long readKeys waits for the rest of an escape
// sequence before deciding the ESC was the escape key.
const escapeGrace = 50 * time.Millisecond

// readKeys decodes keys from r until it fails, sending them to keys. Reads
// happen on their own goroutine so a sequence split across reads can be
// completed, while a lone ESC still arrives promptly.
func readKeys(r io.Reader, keys chan<- Key, done <-chan struct{}) {
	chunks := make(chan []byte, 8)
	go func() {
		defer close(chunks)
		buf := make([]byte, 256)
		for {
			n, err := r.Read(buf)
			if n > 0 {
				b := make([]byte, n)
				copy(b, buf[:n])
				select {
				case chunks <- b:
				case <-done:
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	var pending []byte
	// flush emits every complete key in pending. With force set, an
	// incomplete sequence is resolved as a bare escape key. It reports
	// whether the reader should keep going.
	flush := func(force bool) bool {
		for len(pending) > 0 {
			k, used, ok := decodeKey(pending)
			if used == 0 {
				if !force {
					return true
				}
				k, used, ok = Key{Type: KeyEsc}, 1, true
			}
			pending = pending[used:]
			if !ok {
				continue
			}
			select {
			case keys <- k:
			case <-done:
				return false
			}
		}
		return true
	}

	for {
		var grace <-chan time.Time
		if len(pending) > 0 {
			grace = time.After(escapeGrace)
		}
		select {
		case b, open := <-chunks:
			if !open {
				flush(true)
				return
			}
			pending = append(pending, b...)
			if !flush(false) {
				return
			}
		case <-grace:
			if !flush(true) {
				return
			}
		case <-done:
			return
		}
	}
}
