// Helpers the inspection commands share.

package cli

import (
	"encoding/json"
	"fmt"

	"github.com/dataGriff/api-caller/internal/httpfile"
	"github.com/dataGriff/api-caller/internal/output"
	"github.com/dataGriff/api-caller/internal/runner"
)

// theme is the shared style set; colour is switched off in root's
// PersistentPreRun when the output is not a terminal.
var theme = output.Default()

func (a *App) writeJSON(v any) error {
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

// single loads the runner and resolves exactly one request.
func (a *App) single(target string) (*runner.Runner, *httpfile.Request, error) {
	r, err := a.newRunner()
	if err != nil {
		return nil, nil, err
	}
	reqs, err := r.Project.Resolve(target)
	if err != nil {
		return nil, nil, runner.Usage(runner.CodeUnknownRequest, err.Error())
	}
	if len(reqs) != 1 {
		return nil, nil, runner.Usage(runner.CodeAmbiguous, fmt.Sprintf("%s names %d requests; pick one with %s#<name>", target, len(reqs), target))
	}
	return r, reqs[0], nil
}

// plural renders "1 file" or "3 files".
func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}
