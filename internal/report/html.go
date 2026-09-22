// Package report renders a self-contained HTML report of an `apic run` or
// an `apic test`: one file, inline CSS, no JavaScript needed to read it,
// light and dark through prefers-color-scheme. It is the artifact to
// attach to a CI run or send to someone who does not read JUnit.
//
// The output is deterministic for a given input: no random ids, and the
// time comes from the caller, so a report can be golden-tested.
package report

import (
	"bytes"
	_ "embed" // for the template source
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/dataGriff/api-caller/internal/runner"
)

//go:embed report.html.tmpl
var source string

// page is the parsed template. .gitattributes keeps the source LF on every
// platform; the fold is the belt to that brace, so a report is
// byte-identical whatever a build machine checked out.
var page = template.Must(template.New("report").Funcs(template.FuncMap{
	"ms":   func(d time.Duration) string { return fmt.Sprintf("%d ms", d.Milliseconds()) },
	"json": prettyJSON,
}).Parse(strings.ReplaceAll(source, "\r\n", "\n")))

// Meta is what the report says about the run as a whole.
type Meta struct {
	Title    string    // "apic run" or "apic test"
	Version  string    // apic version
	Env      string    // environment in effect, "" for none
	Time     time.Time // when the run started; zero means now
	Redacted bool      // --redact was in force
	Project  string    // project root, for the header
}

// A Run report: one section per request, dependencies nested.
type runPage struct {
	Meta
	Passed, Failed int
	Duration       time.Duration
	Requests       []requestView
}

type requestView struct {
	Name, Method, URL, File string
	Line                    int
	OK                      bool
	Status                  int
	StatusText              string
	Duration                time.Duration
	Size                    int
	Attempts                int
	Auth                    string
	RequestHeaders          []kv
	RequestBody             string
	ResponseHeaders         []kv
	ResponseBody            string
	Asserts                 []assertView
	Captures                []kv
	Errors                  []string
	Deps                    []requestView
	Timings                 *runner.Timings
}

type kv struct{ Key, Value string }

type assertView struct {
	Expr, Actual, Expected, Error string
	Pass                          bool
}

// Run writes the HTML report of an `apic run`.
func Run(w io.Writer, meta Meta, results []*runner.Result) error {
	p := runPage{Meta: fill(meta, "apic run")}
	for _, res := range results {
		v := view(res)
		p.Requests = append(p.Requests, v)
		p.Duration += v.Duration
		if v.OK {
			p.Passed++
		} else {
			p.Failed++
		}
	}
	return page.ExecuteTemplate(w, "run", p)
}

func fill(m Meta, title string) Meta {
	if m.Title == "" {
		m.Title = title
	}
	if m.Time.IsZero() {
		m.Time = time.Now()
	}
	m.Time = m.Time.UTC().Truncate(time.Second)
	return m
}

func view(res *runner.Result) requestView {
	v := requestView{Name: res.Request.Name, Method: res.Request.Method, URL: res.Request.DisplayURL(res.Redact), File: res.Request.File, Line: res.Request.Line,
		OK: res.OK, Attempts: res.Attempts, Auth: res.Request.Auth, RequestBody: res.Request.DisplayBody(res.Redact), Errors: res.Errors}
	if v.Name == "" {
		v.Name = fmt.Sprintf("%s#%d", v.File, v.Line)
	}
	for _, h := range res.Request.DisplayHeaders(res.Redact) {
		v.RequestHeaders = append(v.RequestHeaders, kv{h.Name, h.Value})
	}
	if r := res.DisplayResponse(); r != nil {
		v.Status, v.StatusText, v.Duration, v.Size, v.Timings = r.Status, r.StatusText, time.Duration(r.DurationMs)*time.Millisecond, r.Size, r.Timings
		v.ResponseHeaders = sortedKV(r.Headers)
		v.ResponseBody = string(res.DisplayRawBody())
		if runner.IsBinary(res.DisplayRawBody()) {
			v.ResponseBody = fmt.Sprintf("(binary body, %d bytes)", r.Size)
		}
	}
	if res.SavedTo != "" {
		v.Captures = append(v.Captures, kv{"saved to", res.SavedTo})
	}
	for _, a := range res.DisplayAsserts() {
		v.Asserts = append(v.Asserts, assertView{Expr: a.Expr, Actual: a.Actual, Expected: a.Expected, Error: a.Error, Pass: a.Pass})
	}
	v.Captures = sortedKV(res.DisplayCaptures())
	for _, d := range res.Deps {
		v.Deps = append(v.Deps, view(d))
	}
	return v
}

func sortedKV(m map[string]string) []kv {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]kv, 0, len(keys))
	for _, k := range keys {
		out = append(out, kv{k, m[k]})
	}
	return out
}

// prettyJSON indents a JSON body for reading; anything else is returned
// as it is.
func prettyJSON(s string) string {
	t := strings.TrimSpace(s)
	if t == "" || (t[0] != '{' && t[0] != '[') || !json.Valid([]byte(t)) {
		return s
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, []byte(t), "", "  "); err != nil {
		return s
	}
	return buf.String()
}

// A Features report, from the cucumber JSON `apic test` produces.
type featuresPage struct {
	Meta
	Scenarios, Passed, Failed, Skipped, Undefined, Steps int
	Duration                                             time.Duration
	Features                                             []featureView
}

type featureView struct {
	Name, URI string
	Passed    bool
	Scenarios []scenarioView
}

type scenarioView struct {
	Name, Keyword, Status string
	Duration              time.Duration
	Steps                 []stepView
}

type stepView struct {
	Keyword, Name, Status, Error, DocString string
	Rows                                    [][]string
	Duration                                time.Duration
}

// cukeFeature is the subset of godog's cucumber JSON the report reads.
type cukeFeature struct {
	URI      string `json:"uri"`
	Keyword  string `json:"keyword"`
	Name     string `json:"name"`
	Elements []struct {
		Keyword string `json:"keyword"`
		Name    string `json:"name"`
		Type    string `json:"type"`
		Steps   []struct {
			Keyword string `json:"keyword"`
			Name    string `json:"name"`
			Result  struct {
				Status       string `json:"status"`
				Duration     int64  `json:"duration"`
				ErrorMessage string `json:"error_message"`
			} `json:"result"`
			DocString *struct {
				Value string `json:"value"`
			} `json:"doc_string"`
			Rows []struct {
				Cells []string `json:"cells"`
			} `json:"rows"`
		} `json:"steps"`
	} `json:"elements"`
}

// Features writes the HTML report of an `apic test` from its cucumber
// JSON report, which is already masked under --redact.
func Features(w io.Writer, meta Meta, cucumber []byte) error {
	var feats []cukeFeature
	if len(bytes.TrimSpace(cucumber)) > 0 {
		if err := json.Unmarshal(cucumber, &feats); err != nil {
			return fmt.Errorf("parse cucumber report: %w", err)
		}
	}
	p := featuresPage{Meta: fill(meta, "apic test")}
	for _, f := range feats {
		fv := featureView{Name: f.Name, URI: f.URI, Passed: true}
		// godog emits a background element before each scenario it
		// applies to; its steps are shown under that scenario.
		var background []stepView
		for _, el := range f.Elements {
			steps := make([]stepView, 0, len(el.Steps))
			for _, st := range el.Steps {
				sv := stepView{Keyword: strings.TrimSpace(st.Keyword), Name: st.Name, Status: st.Result.Status, Error: st.Result.ErrorMessage, Duration: time.Duration(st.Result.Duration)}
				if st.DocString != nil {
					sv.DocString = st.DocString.Value
				}
				for _, r := range st.Rows {
					sv.Rows = append(sv.Rows, r.Cells)
				}
				steps = append(steps, sv)
			}
			if el.Type == "background" {
				background = steps
				continue
			}
			if el.Type != "scenario" {
				continue
			}
			sc := scenarioView{Name: el.Name, Keyword: el.Keyword, Status: "passed", Steps: append(append([]stepView{}, background...), steps...)}
			background = nil
			ran := false
			for _, sv := range sc.Steps {
				p.Steps++
				sc.Duration += sv.Duration
				switch sv.Status {
				case "failed", "undefined", "pending", "ambiguous":
					if sc.Status == "passed" {
						sc.Status = sv.Status
					}
					if sv.Status == "undefined" {
						p.Undefined++
					}
				case "passed":
					ran = true
				}
			}
			if sc.Status == "passed" && !ran && len(sc.Steps) > 0 {
				sc.Status = "skipped"
			}
			p.Scenarios++
			p.Duration += sc.Duration
			switch sc.Status {
			case "passed":
				p.Passed++
			case "skipped":
				p.Skipped++
			default:
				p.Failed++
				fv.Passed = false
			}
			fv.Scenarios = append(fv.Scenarios, sc)
		}
		p.Features = append(p.Features, fv)
	}
	return page.ExecuteTemplate(w, "features", p)
}
