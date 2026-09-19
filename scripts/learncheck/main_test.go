package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExtractTakesOnlyMarkedFences(t *testing.T) {
	src := "# Title\n\nProse.\n\n<!-- learn -->\n```sh\napic list\n```\n\n```sh\nnot marked\n```\n\n<!-- learn -->\n\n```sh\necho two\necho three\n```\n\n<!-- learn -->\nsome prose in between\n```sh\nnot this one either\n```\n\n    <!-- learn -->\n    ```sh\n    indented\n    ```\n"
	got := Extract("x.md", src)
	if len(got) != 3 {
		t.Fatalf("want 3 blocks, got %d: %+v", len(got), got)
	}
	if got[0].Line != 6 || got[0].Code != "apic list\n" {
		t.Errorf("block 0 = %+v", got[0])
	}
	if got[1].Line != 16 || got[1].Code != "echo two\necho three\n" {
		t.Errorf("block 1 = %+v", got[1])
	}
	if got[2].Code != "indented\n" {
		t.Errorf("indented block should lose its indent: %+v", got[2])
	}
	for _, b := range got {
		if b.File != "x.md" {
			t.Errorf("file = %q", b.File)
		}
	}
}

// TestExtractAcceptsLongerClosingFences pins CommonMark's rule that a
// closing fence may be longer than the opening one: ``` closed by ````
// must end the block rather than swallow the rest of the page.
func TestExtractAcceptsLongerClosingFences(t *testing.T) {
	src := "<!-- learn -->\n```sh\necho one\n````\n\nprose that is not code\n\n<!-- learn -->\n~~~\necho two\n~~~~~\n\n<!-- learn -->\n```\necho ``` inside is fine\n```  \n"
	got := Extract("x.md", src)
	if len(got) != 3 {
		t.Fatalf("want 3 blocks, got %d: %+v", len(got), got)
	}
	if got[0].Code != "echo one\n" || got[1].Code != "echo two\n" || got[2].Code != "echo ``` inside is fine\n" {
		t.Errorf("blocks = %+v", got)
	}
	for _, c := range []struct {
		line, fence string
		want        bool
	}{
		{"```", "```", true}, {"````", "```", true}, {"  ```  ", "```", true}, {"~~~~", "~~~", true},
		{"``", "```", false}, {"```sh", "```", false}, {"~~~", "```", false}, {"echo ```", "```", false},
	} {
		if got := closesFence(c.line, c.fence); got != c.want {
			t.Errorf("closesFence(%q, %q) = %v, want %v", c.line, c.fence, got, c.want)
		}
	}
}

func TestExtractHandlesCRLF(t *testing.T) {
	got := Extract("x.md", "<!-- learn -->\r\n```sh\r\necho hi\r\n```\r\n")
	if len(got) != 1 || got[0].Code != "echo hi\n" {
		t.Fatalf("%+v", got)
	}
}

// TestTemplateRunsEndToEnd builds apic, starts the demo and runs the blocks
// in the course template, which is what CI does for every lesson.
func TestTemplateRunsEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binary and serves the demo")
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	if err := run(filepath.Join("docs", "learn"), false); err != nil {
		t.Fatal(err)
	}
}
