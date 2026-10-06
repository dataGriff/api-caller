package lsp

import (
	"encoding/json"
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf16"
)

// The parts of the protocol apic uses, as the specification names them.

type position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

type lspRange struct {
	Start position `json:"start"`
	End   position `json:"end"`
}

type textDocumentItem struct {
	URI     string `json:"uri"`
	Version int    `json:"version"`
	Text    string `json:"text"`
}

type textDocumentIdentifier struct {
	URI string `json:"uri"`
}

type textDocumentPositionParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
	Position     position               `json:"position"`
}

type didOpenParams struct {
	TextDocument textDocumentItem `json:"textDocument"`
}

type didChangeParams struct {
	TextDocument struct {
		URI     string `json:"uri"`
		Version int    `json:"version"`
	} `json:"textDocument"`
	ContentChanges []struct {
		Range *lspRange `json:"range,omitempty"`
		Text  string    `json:"text"`
	} `json:"contentChanges"`
}

type didCloseParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
}

type didChangeWatchedFilesParams struct {
	Changes []struct {
		URI  string `json:"uri"`
		Type int    `json:"type"`
	} `json:"changes"`
}

type initializeParams struct {
	RootURI          string `json:"rootUri"`
	RootPath         string `json:"rootPath"`
	WorkspaceFolders []struct {
		URI string `json:"uri"`
	} `json:"workspaceFolders"`
	InitializationOptions struct {
		Env string `json:"env"`
	} `json:"initializationOptions"`
	Capabilities struct {
		General struct {
			PositionEncodings []string `json:"positionEncodings"`
		} `json:"general"`
		TextDocument struct {
			Completion struct {
				CompletionItem struct {
					SnippetSupport bool `json:"snippetSupport"`
				} `json:"completionItem"`
			} `json:"completion"`
		} `json:"textDocument"`
		Workspace struct {
			DidChangeWatchedFiles struct {
				DynamicRegistration bool `json:"dynamicRegistration"`
			} `json:"didChangeWatchedFiles"`
		} `json:"workspace"`
	} `json:"capabilities"`
}

type diagnostic struct {
	Range           lspRange         `json:"range"`
	Severity        int              `json:"severity"`
	Code            string           `json:"code,omitempty"`
	CodeDescription *codeDescription `json:"codeDescription,omitempty"`
	Source          string           `json:"source"`
	Message         string           `json:"message"`
}

type codeDescription struct {
	Href string `json:"href"`
}

type publishDiagnosticsParams struct {
	URI         string       `json:"uri"`
	Diagnostics []diagnostic `json:"diagnostics"`
}

type markupContent struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

type hover struct {
	Contents markupContent `json:"contents"`
	Range    *lspRange     `json:"range,omitempty"`
}

type textEdit struct {
	Range   lspRange `json:"range"`
	NewText string   `json:"newText"`
}

type completionItem struct {
	Label            string         `json:"label"`
	Kind             int            `json:"kind,omitempty"`
	Detail           string         `json:"detail,omitempty"`
	Documentation    *markupContent `json:"documentation,omitempty"`
	SortText         string         `json:"sortText,omitempty"`
	FilterText       string         `json:"filterText,omitempty"`
	InsertTextFormat int            `json:"insertTextFormat,omitempty"`
	TextEdit         *textEdit      `json:"textEdit,omitempty"`
}

type completionList struct {
	IsIncomplete bool             `json:"isIncomplete"`
	Items        []completionItem `json:"items"`
}

type command struct {
	Title     string `json:"title"`
	Command   string `json:"command"`
	Arguments []any  `json:"arguments,omitempty"`
}

type codeLens struct {
	Range   lspRange `json:"range"`
	Command *command `json:"command,omitempty"`
}

type executeCommandParams struct {
	Command   string            `json:"command"`
	Arguments []json.RawMessage `json:"arguments"`
}

type showMessageParams struct {
	Type    int    `json:"type"`
	Message string `json:"message"`
}

// Diagnostic severities, completion item kinds, insert text formats and
// message types, as numbered by the specification.
const (
	severityError   = 1
	severityWarning = 2

	kindKeyword  = 14
	kindSnippet  = 15
	kindVariable = 6
	kindField    = 5
	kindOperator = 24
	kindModule   = 9

	formatPlain   = 1
	formatSnippet = 2

	messageError   = 1
	messageWarning = 2
	messageInfo    = 3
	messageLog     = 4
)

// uriToPath turns a file:// URI into a local path; ok is false for any
// other scheme (an untitled buffer, a remote file).
func uriToPath(uri string) (string, bool) {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "file" {
		return "", false
	}
	p := u.Path
	if runtime.GOOS == "windows" {
		// file:///C:/dir/f.http has the path /C:/dir/f.http.
		p = strings.TrimPrefix(p, "/")
	}
	return filepath.Clean(filepath.FromSlash(p)), true
}

// pathToURI is the file:// URI of a local path.
func pathToURI(path string) string {
	p := filepath.ToSlash(path)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p // C:/dir becomes /C:/dir
	}
	return (&url.URL{Scheme: "file", Path: p}).String()
}

// units converts between the byte columns apic reports and the columns
// the client counts in: UTF-16 code units unless it agreed to UTF-8.
type units struct{ utf8 bool }

// toClient is the client column of byte offset col (0-based) in line.
func (u units) toClient(line string, col int) int {
	if col > len(line) {
		col = len(line)
	}
	if col < 0 {
		col = 0
	}
	if u.utf8 {
		return col
	}
	n := 0
	for _, r := range line[:col] {
		n += utf16.RuneLen(r)
	}
	return n
}

// toByte is the byte offset in line of client column char.
func (u units) toByte(line string, char int) int {
	if u.utf8 {
		return min(max(char, 0), len(line))
	}
	n := 0
	for i, r := range line {
		if n >= char {
			return i
		}
		w := utf16.RuneLen(r)
		if w < 0 {
			w = 1 // an invalid rune counts as one unit, as editors show it
		}
		n += w
	}
	return len(line)
}

// lines splits text the way editors number lines: on \n, with a trailing
// \r dropped from each line.
func lines(text string) []string {
	ls := strings.Split(text, "\n")
	for i, l := range ls {
		ls[i] = strings.TrimSuffix(l, "\r")
	}
	return ls
}

// endOf is the position just past the last character of text.
func (u units) endOf(text string) position {
	ls := lines(text)
	last := ls[len(ls)-1]
	return position{Line: len(ls) - 1, Character: u.toClient(last, len(last))}
}
