package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dataGriff/api-caller/internal/runner"
)

// TestErrorsPageIsCurrent fails when the catalogue changed without
// `task docs:errors`, which is how CI catches a stale docs/errors.md.
func TestErrorsPageIsCurrent(t *testing.T) {
	current, err := os.ReadFile(filepath.Join("..", "..", "docs", "errors.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.ReplaceAll(string(current), "\r\n", "\n") != Markdown() {
		t.Fatal("docs/errors.md is stale; run `task docs:errors`")
	}
}

// Every code has an anchor that Code.URL points at, and the page's
// example URL is the real one.
func TestAnchors(t *testing.T) {
	md := Markdown()
	for _, e := range runner.Catalogue {
		id := e.Code.URL()[strings.Index(e.Code.URL(), "#")+1:]
		if !strings.Contains(md, `<a id="`+id+`"></a>`) {
			t.Errorf("%s: no anchor %s", e.Code, id)
		}
	}
	if !strings.Contains(md, runner.CodeMissingVariable.URL()) {
		t.Error("the example URL does not match Code.URL")
	}
}
