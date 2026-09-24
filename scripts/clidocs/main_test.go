package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/spf13/pflag"
)

// TestReferenceIsCurrent fails when a flag or command changed without
// `task docs:cli`, which is how CI catches a stale docs/cli.md.
func TestReferenceIsCurrent(t *testing.T) {
	path := filepath.Join("..", "..", "docs", "cli.md")
	current, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want, err := Update(string(current), Markdown(Tree()))
	if err != nil {
		t.Fatal(err)
	}
	// A checkout with autocrlf (the Windows runner) is not staleness.
	if strings.ReplaceAll(string(current), "\r\n", "\n") != want {
		t.Fatal("docs/cli.md is stale; run `task docs:cli`")
	}
	if !strings.Contains(want, "\n## apic run\n") {
		t.Fatal("the hand-written sections must survive")
	}
}

// Every flag `run` registers is in its section, and so on for every
// command.
func TestEveryFlagIsListed(t *testing.T) {
	root := Tree()
	md := Markdown(root)
	for _, c := range commands(root) {
		start := strings.Index(md, "\n### "+c.CommandPath()+"\n")
		if start < 0 {
			t.Errorf("no section for %s", c.CommandPath())
			continue
		}
		section := md[start+1:]
		if next := strings.Index(section[4:], "\n### "); next >= 0 {
			section = section[:next+4]
		}
		c.LocalNonPersistentFlags().VisitAll(func(f *pflag.Flag) {
			if f.Name != "help" && !strings.Contains(section, "--"+f.Name+"`") && !strings.Contains(section, "--"+f.Name+" <") {
				t.Errorf("%s: --%s missing", c.CommandPath(), f.Name)
			}
		})
	}
	for _, name := range []string{"--output <string>", "--report <string>", "--retry <string>", "--keep-going", "-e, --env <string>"} {
		if !strings.Contains(md, "`"+name+"`") {
			t.Errorf("reference lacks %s", name)
		}
	}
}

// cobra reads the first `backticked` word of a usage string as the value's
// name, so `--output >>! file` once appeared in help. Every value name must
// be a word.
func TestValueNamesAreWords(t *testing.T) {
	word := regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]*$`)
	root := Tree()
	for _, c := range append(commands(root), root) {
		c.Flags().VisitAll(func(f *pflag.Flag) {
			if name, _ := pflag.UnquoteUsage(f); name != "" && !word.MatchString(name) {
				t.Errorf("%s --%s: value name %q comes from backticks in its usage", c.CommandPath(), f.Name, name)
			}
		})
	}
}

func TestManPages(t *testing.T) {
	root := Tree()
	pages := Man(root)
	for _, c := range commands(root) {
		if _, ok := pages[manName(c)+".1"]; !ok {
			t.Errorf("no page for %s", c.CommandPath())
		}
	}
	run := pages["apic-run.1"]
	for _, want := range []string{`.TH "APIC-RUN" 1`, ".SH NAME\napic\\-run \\- ", "\\-\\-output <string>", ".SH EXAMPLES", "apic(1)", ".SH OPTIONS INHERITED FROM PARENT COMMANDS"} {
		if !strings.Contains(run, want) {
			t.Errorf("apic-run.1 lacks %q:\n%s", want, run)
		}
	}
	if !strings.Contains(pages["apic.1"], ".SH GLOBAL OPTIONS") || !strings.Contains(pages["apic.1"], "apic\\-run(1)") {
		t.Errorf("apic.1:\n%s", pages["apic.1"])
	}
	for name, text := range pages {
		for i, l := range strings.Split(text, "\n") {
			if strings.HasPrefix(l, "'") {
				t.Errorf("%s:%d starts with a quote, which roff reads as a request", name, i+1)
			}
		}
	}
}

func TestUpdate(t *testing.T) {
	gen := Begin + "\nnew\n" + End + "\n"
	got, err := Update("# Page\n\ntext\n", gen)
	if err != nil || got != "# Page\n\ntext\n\n"+gen {
		t.Fatalf("append: %q %v", got, err)
	}
	got, err = Update("# Page\n\n"+Begin+"\nold\n"+End+"\n", gen)
	if err != nil || got != "# Page\n\n"+gen {
		t.Fatalf("replace: %q %v", got, err)
	}
	if _, err := Update("# Page\n"+End+"\n"+Begin+"\n", gen); err == nil {
		t.Fatal("markers out of order")
	}
}

// Usage text reaches a Markdown table, where <word> is an HTML tag.
func TestCellEscapesHTML(t *testing.T) {
	if got, want := cell("send \"<attempts> [interval]\" | a & b\nnext"), `send "&lt;attempts&gt; [interval]" \| a &amp; b next`; got != want {
		t.Fatalf("cell = %q; want %q", got, want)
	}
	if !strings.Contains(Markdown(Tree()), "&lt;attempts&gt;") {
		t.Fatal("run --retry's usage is not escaped in the reference")
	}
}
