package httpfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFormatGolden(t *testing.T) {
	ins, err := filepath.Glob("testdata/fmt/*.in.http")
	if err != nil || len(ins) == 0 {
		t.Fatalf("no golden pairs: %v", err)
	}
	for _, in := range ins {
		src, err := os.ReadFile(in)
		if err != nil {
			t.Fatal(err)
		}
		want, err := os.ReadFile(strings.TrimSuffix(in, ".in.http") + ".out.http")
		if err != nil {
			t.Fatal(err)
		}
		got := Format(string(src))
		// A checkout with autocrlf (the Windows runner) turns the committed
		// LF golden file into CRLF; the formatter's output is always LF.
		if got != strings.ReplaceAll(string(want), "\r\n", "\n") {
			t.Errorf("%s:\n--- got ---\n%s--- want ---\n%s", in, got, want)
		}
		if again := Format(got); again != got {
			t.Errorf("%s: not idempotent:\n%s", in, again)
		}
		// The canonical form parses to the same requests.
		before, _ := Parse(in, string(src))
		after, _ := Parse(in, got)
		if len(before.Requests) != len(after.Requests) {
			t.Fatalf("%s: %d requests before, %d after", in, len(before.Requests), len(after.Requests))
		}
		for i := range before.Requests {
			b, a := before.Requests[i], after.Requests[i]
			if b.Name != a.Name || b.Method != a.Method || b.URL != a.URL || len(b.Headers) != len(a.Headers) || len(b.Directives) != len(a.Directives) || b.BodyFile != a.BodyFile || (b.Body != a.Body && !isJSON(b.Body)) {
				t.Errorf("%s: request %d changed meaning: %+v -> %+v", in, i, b, a)
			}
		}
	}
}

// isJSON reports whether a body is one JSON document, the only kind the
// formatter rewrites.
func isJSON(body string) bool {
	t := strings.TrimSpace(body)
	return t != "" && (t[0] == '{' || t[0] == '[') && !strings.Contains(t, "{{")
}

// Every .http file in the repository formats to itself once formatted, and
// keeps its requests: the formatter must be safe to run on real projects.
func TestFormatIsIdempotentOnRepositoryFiles(t *testing.T) {
	var files []string
	for _, root := range []string{"../../examples", "../../internal/demoapi/project", "testdata"} {
		_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err == nil && !d.IsDir() && strings.HasSuffix(path, ".http") && !strings.Contains(path, "/fmt/") {
				files = append(files, path)
			}
			return nil
		})
	}
	if len(files) < 5 {
		t.Fatalf("found only %v", files)
	}
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		once := Format(string(src))
		if twice := Format(once); twice != once {
			t.Errorf("%s: not idempotent", f)
		}
		before, _ := Parse(f, string(src))
		after, _ := Parse(f, once)
		if len(before.Requests) != len(after.Requests) {
			t.Errorf("%s: %d requests before formatting, %d after", f, len(before.Requests), len(after.Requests))
		}
	}
	if Format("") != "" || Format("\n\n") != "" {
		t.Error("empty input stays empty")
	}
	if got := Format("GET http://x\r\n"); got != "GET http://x\n" {
		t.Errorf("CRLF: %q", got)
	}
}
