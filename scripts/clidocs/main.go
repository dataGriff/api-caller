// Command clidocs generates the "Commands and flags" section at the end of
// docs/cli.md and the man pages, from apic's own command tree, so the
// reference cannot drift from what the binary accepts. The hand-written
// parts of docs/cli.md (targets, exit codes, JSON shapes, examples) stay
// as they are; only the text between the markers is replaced.
//
//	go run ./scripts/clidocs            rewrite docs/cli.md (task docs:cli)
//	go run ./scripts/clidocs -check     exit 1 when docs/cli.md is stale
//	go run ./scripts/clidocs -man man   write man/apic.1, man/apic-run.1, …
//
// It lives under scripts/ rather than in the binary: nothing at run time
// needs it, and TestReferenceIsCurrent fails when the committed page is
// stale, so CI catches a flag added without regenerating.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/dataGriff/api-caller/internal/cli"
)

// The markers around the generated section of docs/cli.md.
const (
	Begin = "<!-- BEGIN GENERATED: go run ./scripts/clidocs (task docs:cli) rewrites this section; edit the flags in internal/cli instead -->"
	End   = "<!-- END GENERATED -->"
)

func main() {
	check := flag.Bool("check", false, "exit 1 if the page is stale instead of rewriting it")
	page := flag.String("md", filepath.Join("docs", "cli.md"), "the page holding the generated section")
	man := flag.String("man", "", "write man pages into this directory instead of updating the page")
	flag.Parse()
	root := Tree()
	if *man != "" {
		// Only the man pages: rendering them changes the tree's usage
		// lines, so the page must not be written from the same tree (and
		// goreleaser, which runs this, must not touch the docs).
		if err := os.MkdirAll(*man, 0o755); err != nil { //nolint:gosec // a build output directory
			log.Fatal(err)
		}
		for name, text := range Man(root) {
			if err := os.WriteFile(filepath.Join(*man, name), []byte(text), 0o644); err != nil { //nolint:gosec // man pages are public
				log.Fatal(err)
			}
		}
		return
	}
	current, err := os.ReadFile(*page)
	if err != nil {
		log.Fatal(err)
	}
	updated, err := Update(string(current), Markdown(root))
	if err != nil {
		log.Fatalf("%s: %v", *page, err)
	}
	if *check {
		if updated != strings.ReplaceAll(string(current), "\r\n", "\n") {
			fmt.Fprintf(os.Stderr, "%s is stale; run `task docs:cli`\n", *page)
			os.Exit(1)
		}
		return
	}
	if err := os.WriteFile(*page, []byte(updated), 0o644); err != nil { //nolint:gosec // a docs page
		log.Fatal(err)
	}
}

// Tree is apic's command tree as the binary builds it, with the
// completion command cobra adds when it runs.
func Tree() *cobra.Command {
	root := cli.New().Root
	root.InitDefaultCompletionCmd()
	return root
}

// commands is every visible command under root, depth first, by name.
func commands(root *cobra.Command) []*cobra.Command {
	var out []*cobra.Command
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		for _, sub := range c.Commands() {
			if sub.Hidden || sub.Name() == "help" {
				continue
			}
			out = append(out, sub)
			walk(sub)
		}
	}
	walk(root)
	return out
}

// Update replaces the generated section of page, between the markers, or
// appends it when the page has none yet.
func Update(page, generated string) (string, error) {
	page = strings.ReplaceAll(page, "\r\n", "\n")
	b, e := strings.Index(page, Begin), strings.Index(page, End)
	switch {
	case b < 0 && e < 0:
		return strings.TrimRight(page, "\n") + "\n\n" + generated, nil
	case b < 0 || e < b:
		return "", fmt.Errorf("the generated-section markers are out of order or one is missing")
	}
	return page[:b] + generated + strings.TrimLeft(page[e+len(End):], "\n"), nil
}

// Markdown renders the section: the global flags once, then each command
// with its synopsis, usage and own flags.
func Markdown(root *cobra.Command) string {
	var b strings.Builder
	b.WriteString(Begin + "\n\n")
	b.WriteString("## Commands and flags\n\n")
	b.WriteString("Generated from apic's own command tree, so it lists exactly what the\n")
	b.WriteString("binary accepts. The sections above explain each command; this is every\n")
	b.WriteString("flag in one place. `man apic` and `man apic-run` show the same, where\n")
	b.WriteString("the release archive's `man/` pages are installed.\n\n")
	b.WriteString("### Global flags\n\nThese work with every command.\n\n")
	flagTable(&b, root.PersistentFlags())
	for _, c := range commands(root) {
		fmt.Fprintf(&b, "\n### %s\n\n%s\n\n", c.CommandPath(), sentence(c.Short))
		fmt.Fprintf(&b, "```\n%s\n```\n\n", c.UseLine())
		if c.HasAvailableLocalFlags() {
			flagTable(&b, c.LocalNonPersistentFlags())
		} else {
			b.WriteString("No flags of its own.\n")
		}
	}
	b.WriteString("\n" + End + "\n")
	return b.String()
}

func flagTable(b *strings.Builder, fs *pflag.FlagSet) {
	b.WriteString("| Flag | Meaning |\n|---|---|\n")
	fs.VisitAll(func(f *pflag.Flag) {
		if f.Hidden || f.Name == "help" {
			return
		}
		fmt.Fprintf(b, "| `%s` | %s |\n", flagName(f), cell(flagUsage(f)))
	})
}

// flagName is `-C, --dir <string>`.
func flagName(f *pflag.Flag) string {
	name, _ := pflag.UnquoteUsage(f)
	s := "--" + f.Name
	if f.Shorthand != "" {
		s = "-" + f.Shorthand + ", " + s
	}
	if name != "" {
		s += " <" + typeWord(name) + ">"
	}
	return s
}

func typeWord(t string) string {
	switch t {
	case "stringArray", "stringSlice", "strings":
		return "string"
	}
	return t
}

// flagUsage is the usage text with the default, when it is not the zero value.
func flagUsage(f *pflag.Flag) string {
	_, usage := pflag.UnquoteUsage(f)
	switch f.DefValue {
	case "", "false", "0", "[]", "0s":
	default:
		usage += fmt.Sprintf(" (default `%s`)", f.DefValue)
	}
	return usage
}

// cell makes usage text safe in a table cell: a | would end the cell, and
// <attempts> would be read as an HTML tag and vanish from the page.
var cellEscaper = strings.NewReplacer("|", `\|`, "\n", " ", "&", "&amp;", "<", "&lt;", ">", "&gt;")

func cell(s string) string {
	return cellEscaper.Replace(s)
}

func sentence(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || strings.HasSuffix(s, ".") {
		return s
	}
	return s + "."
}

// Man renders one man page per command, named the way man(1) finds them:
// apic.1, apic-run.1, apic-session-clear.1.
func Man(root *cobra.Command) map[string]string {
	pages := map[string]string{"apic.1": manPage(root)}
	for _, c := range commands(root) {
		pages[manName(c)+".1"] = manPage(c)
	}
	return pages
}

func manName(c *cobra.Command) string {
	return strings.ReplaceAll(c.CommandPath(), " ", "-")
}

func manPage(c *cobra.Command) string {
	var b bytes.Buffer
	name := manName(c)
	fmt.Fprintf(&b, ".TH %q 1 \"\" \"apic\" \"apic manual\"\n", strings.ToUpper(name))
	fmt.Fprintf(&b, ".SH NAME\n%s \\- %s\n", roff(name), roff(c.Short))
	fmt.Fprintf(&b, ".SH SYNOPSIS\n.B %s\n", roff(c.UseLine()))
	desc := c.Long
	if desc == "" {
		desc = c.Short
	}
	b.WriteString(".SH DESCRIPTION\n")
	for _, para := range strings.Split(strings.TrimSpace(desc), "\n\n") {
		fmt.Fprintf(&b, ".PP\n%s\n", roffLines(para))
	}
	manFlags(&b, "OPTIONS", c.LocalNonPersistentFlags())
	if c == c.Root() {
		manFlags(&b, "GLOBAL OPTIONS", c.PersistentFlags())
	} else {
		manFlags(&b, "OPTIONS INHERITED FROM PARENT COMMANDS", c.InheritedFlags())
	}
	if ex := strings.TrimSpace(c.Example); ex != "" {
		fmt.Fprintf(&b, ".SH EXAMPLES\n.PP\n.nf\n%s\n.fi\n", roffLines(ex))
	}
	var see []string
	if c.HasParent() {
		see = append(see, manName(c.Parent())+"(1)")
	}
	for _, sub := range c.Commands() {
		if !sub.Hidden && sub.Name() != "help" {
			see = append(see, manName(sub)+"(1)")
		}
	}
	sort.Strings(see)
	if len(see) > 0 {
		fmt.Fprintf(&b, ".SH SEE ALSO\n%s\n", roff(strings.Join(see, ", ")))
	}
	b.WriteString(".PP\nhttps://datagriff.github.io/api-caller/cli/\n")
	return b.String()
}

func manFlags(b *bytes.Buffer, title string, fs *pflag.FlagSet) {
	var rows []string
	fs.VisitAll(func(f *pflag.Flag) {
		if f.Hidden || f.Name == "help" {
			return
		}
		rows = append(rows, fmt.Sprintf(".TP\n.B %s\n%s\n", roff(flagName(f)), roffLines(strings.ReplaceAll(flagUsage(f), "`", ""))))
	})
	if len(rows) == 0 {
		return
	}
	fmt.Fprintf(b, ".SH %s\n%s", title, strings.Join(rows, ""))
}

// roff escapes text for a man page: backslashes and hyphens.
func roff(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\e`), "-", `\-`)
}

// roffLines escapes a block and keeps a line that starts with a dot or a
// quote from being read as a request.
func roffLines(s string) string {
	lines := strings.Split(roff(s), "\n")
	for i, l := range lines {
		if strings.HasPrefix(l, ".") || strings.HasPrefix(l, "'") {
			lines[i] = `\&` + l
		}
	}
	return strings.Join(lines, "\n")
}
