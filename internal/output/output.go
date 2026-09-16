// Package output renders run results for humans (coloured, readable) and for
// machines (one JSON object per result). The string-returning renderers in
// render.go are shared with the terminal UI; the functions here are the
// io.Writer wrappers the CLI uses.
package output

import (
	"bytes"
	"encoding/json"
	"io"

	"github.com/dataGriff/api-caller/internal/runner"
)

// JSON writes one result as a single JSON line.
func JSON(w io.Writer, res *runner.Result) error {
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

// Human writes a readable report of a result.
func Human(w io.Writer, res *runner.Result, verbose bool) {
	_, _ = io.WriteString(w, Result(Default(), res, Options{Verbose: verbose}))
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
