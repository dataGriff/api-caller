// Package output renders run results for humans (coloured, readable) and for
// machines (one JSON object per result). The string-returning renderers in
// render.go are shared with the terminal UI; the functions here are the
// io.Writer wrappers the CLI uses.
package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/dataGriff/api-caller/internal/runner"
)

// JSON writes one result as a single JSON line. The results of requests
// `# @ref` ran first are written as lines of their own, before it, so a
// consumer of `apic run --json` sees exactly one object per request sent;
// the object itself carries no ran_first.
func JSON(w io.Writer, res *runner.Result) error {
	for _, dep := range res.Deps {
		if err := JSON(w, dep); err != nil {
			return err
		}
	}
	flat := *res
	flat.Deps = nil
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc.Encode(&flat)
}

// JSONNested writes one result as a single JSON line with the results of
// its `# @ref` dependencies nested under ran_first.
func JSONNested(w io.Writer, res *runner.Result) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc.Encode(res)
}

// Body writes only the response body, pretty-printed when it is JSON.
func Body(w io.Writer, res *runner.Result) {
	if res.Raw() == nil {
		return
	}
	_, _ = w.Write(prettyJSON(res.DisplayRawBody()))
	_, _ = io.WriteString(w, "\n")
}

// Human writes a readable report of a result, after the reports of the
// requests `# @ref` ran first, each under a line saying why it ran.
func Human(w io.Writer, res *runner.Result, verbose bool) {
	Deps(w, res, verbose)
	_, _ = io.WriteString(w, Result(Default(), res, Options{Verbose: verbose}))
}

// Deps writes the reports of the requests `# @ref` ran before res, each
// followed by a line saying why it ran.
func Deps(w io.Writer, res *runner.Result, verbose bool) {
	t := Default()
	for _, dep := range res.Deps {
		Human(w, dep, verbose)
		_, _ = io.WriteString(w, t.Dim.Render(fmt.Sprintf("↳ ran %s first (# @ref)", resultName(dep)))+"\n\n")
	}
}

// Summary writes the per-request table and totals of a flow.
func Summary(w io.Writer, results []*runner.Result) {
	_, _ = io.WriteString(w, "\n"+SummaryTable(Default(), results))
}

func isJSON(b []byte) bool {
	t := bytes.TrimSpace(b)
	return len(t) > 0 && (t[0] == '{' || t[0] == '[') && json.Valid(t)
}

func prettyJSON(b []byte) []byte {
	if !isJSON(b) {
		return b
	}
	var out bytes.Buffer
	if err := json.Indent(&out, bytes.TrimSpace(b), "", "  "); err != nil {
		return b
	}
	return out.Bytes()
}
