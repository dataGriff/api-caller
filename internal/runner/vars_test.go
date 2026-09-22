package runner

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/dataGriff/api-caller/internal/project"
)

// The built-ins, with the clock pinned: REST Client's offsets and
// $localDatetime, JetBrains' $random family, and $projectRoot.
func TestBuiltins(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.http"), []byte("GET http://x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := project.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	r, err := New(p, Options{NoSession: true})
	if err != nil {
		t.Fatal(err)
	}
	// A fixed instant in a zone that is not UTC, so local and UTC differ.
	zone := time.FixedZone("test", 2*3600)
	fixed := time.Date(2026, 3, 31, 10, 30, 0, 0, zone)
	r.Now = func() time.Time { return fixed }
	exact := map[string]string{
		"$timestamp":                        "1774945800",
		"$timestamp -1 d":                   "1774859400",
		"$timestamp 2 h":                    "1774953000",
		"$timestamp 30 m":                   "1774947600",
		"$timestamp -10 s":                  "1774945790",
		"$timestamp 1 w":                    "1775550600",
		"$timestamp 1 M":                    "1777624200", // 31 March + 1 month: Go rolls to 1 May
		"$timestamp 1 y":                    "1806481800",
		"$timestamp 500 ms":                 "1774945800",
		"$isoTimestamp":                     "2026-03-31T08:30:00Z",
		"$datetime":                         "2026-03-31T08:30:00Z",
		"$datetime iso8601":                 "2026-03-31T08:30:00Z",
		"$datetime rfc1123":                 "Tue, 31 Mar 2026 08:30:00 UTC",
		"$datetime iso8601 1 h":             "2026-03-31T09:30:00Z",
		`$datetime "2006-01-02"`:            "2026-03-31",
		`$datetime "2006-01-02 15:04" -1 d`: "2026-03-30 08:30",
		`$datetime '2006-01-02' 1 Q`:        "2026-07-01", // 31 March + 3 months rolls too
		"$localDatetime":                    "2026-03-31T10:30:00+02:00",
		"$localDatetime rfc1123":            "Tue, 31 Mar 2026 10:30:00 test",
		`$localDatetime "15:04" 2 h`:        "12:30",
		"$projectRoot":                      p.Root,
	}
	for expr, want := range exact {
		got, ok, _, err := r.builtin(expr)
		if err != nil || !ok || got != want {
			t.Errorf("{{%s}} = %q, %v, %v; want %q", expr, got, ok, err, want)
		}
	}
	shaped := map[string]*regexp.Regexp{
		"$random.integer":          regexp.MustCompile(`^\d{1,3}$`),
		"$random.integer(1, 3)":    regexp.MustCompile(`^[12]$`),
		"$random.integer(5,6)":     regexp.MustCompile(`^5$`),
		"$random.float":            regexp.MustCompile(`^\d{1,3}\.\d{3}$`),
		"$random.float(1.5, 1.6)":  regexp.MustCompile(`^1\.5\d\d$`),
		"$random.alphabetic":       regexp.MustCompile(`^[A-Za-z]{10}$`),
		"$random.alphabetic(3)":    regexp.MustCompile(`^[A-Za-z]{3}$`),
		"$random.alphanumeric(12)": regexp.MustCompile(`^[A-Za-z0-9]{12}$`),
		"$random.hexadecimal(8)":   regexp.MustCompile(`^[0-9a-f]{8}$`),
		"$random.email":            regexp.MustCompile(`^[a-z]{8}@example\.com$`),
		"$random.uuid":             regexp.MustCompile(`^[0-9a-f-]{36}$`),
		"$uuid":                    regexp.MustCompile(`^[0-9a-f-]{36}$`),
		"$randomInt 1 3":           regexp.MustCompile(`^[12]$`),
	}
	for expr, re := range shaped {
		got, ok, _, err := r.builtin(expr)
		if err != nil || !ok || !re.MatchString(got) {
			t.Errorf("{{%s}} = %q, %v, %v; want %s", expr, got, ok, err, re)
		}
	}
	bad := map[string]string{
		"$timestamp -1":          "offset",
		"$timestamp 1 fortnight": "unknown offset unit",
		"$timestamp x d":         "not an integer",
		"$datetime rfc1123 1":    "expected",
		"$random.integer(3, 1)":  "max greater than min",
		"$random.integer(1)":     "takes (min, max)",
		"$random.alphabetic(x)":  "one length",
		"$random.email(1)":       "no arguments",
		"$random.nope":           "unknown built-in $random.nope",
		"$nope":                  "unknown built-in $nope",
	}
	for expr, want := range bad {
		_, _, _, err := r.builtin(expr)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("{{%s}}: err = %v; want it to mention %q", expr, err, want)
		}
	}
	// A bad offset is a usage error that names the placeholder and the line.
	if err := os.WriteFile(filepath.Join(dir, "a.http"), []byte("GET http://x/{{$timestamp 1 fortnight}}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	p, _ = project.Load(dir)
	r, _ = New(p, Options{NoSession: true})
	req := p.Requests()[0]
	_, err = r.Run(context.Background(), req)
	if err == nil || !strings.Contains(err.Error(), "a.http:1") || !strings.Contains(err.Error(), "{{$timestamp 1 fortnight}}") || ExitCode(err) != ExitUsage {
		t.Fatalf("err = %v", err)
	}
}
