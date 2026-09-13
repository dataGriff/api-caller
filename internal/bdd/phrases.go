package bdd

import (
	"context"
	"fmt"

	"github.com/cucumber/godog"

	"github.com/dataGriff/api-caller/internal/httpfile"
	"github.com/dataGriff/api-caller/internal/phrase"
	"github.com/dataGriff/api-caller/internal/project"
)

// compiledPhrase is a `# @step` directive ready to register. It keeps the
// request itself: a file/name target string could be misread when a file
// name contains the `#` delimiter.
type compiledPhrase struct {
	phrase *phrase.Phrase
	req    *httpfile.Request
}

// compilePhrases validates every `# @step` directive in the project before
// any scenario runs, so a bad phrase is a usage error even when a tag
// filter selects no scenarios. Equivalent matchers are rejected.
func compilePhrases(p *project.Project) ([]compiledPhrase, error) {
	var out []compiledPhrase
	for _, req := range p.Requests() {
		for _, d := range req.Directives {
			if d.Key != "step" {
				continue
			}
			text := d.Value
			ph, err := phrase.Parse(text)
			if err != nil {
				return nil, fmt.Errorf("%s:%d: %w", req.File.Path, d.Line, err)
			}
			if err := ph.ConflictsWithBuiltin(); err != nil {
				return nil, fmt.Errorf("%s:%d: %w", req.File.Path, d.Line, err)
			}
			for _, prev := range out {
				if ph.ConflictsWith(prev.phrase) {
					return nil, fmt.Errorf("%s:%d: @step %q is ambiguous with @step %q on %s (run `apic validate`)", req.File.Path, d.Line, text, prev.phrase.Text, prev.req.ID())
				}
			}
			out = append(out, compiledPhrase{phrase: ph, req: req})
		}
	}
	return out, nil
}

// registerPhrases adds compiled phrases as steps that run their request
// with the phrase's parameters as variables.
func registerPhrases(sc *godog.ScenarioContext, phrases []compiledPhrase) {
	for _, cp := range phrases {
		ph, req := cp.phrase, cp.req
		run := func(ctx context.Context, args []string) error {
			s, err := from(ctx)
			if err != nil {
				return err
			}
			vars := ph.Values(args)
			for k, v := range vars {
				if vars[k], err = s.render(v); err != nil {
					return err
				}
			}
			return s.runRequests(ctx, []*httpfile.Request{req}, vars)
		}
		sc.Step(ph.Regex, handlerFor(len(ph.Params), run))
	}
}

// handlerFor adapts a []string handler to the fixed-arity function godog expects.
func handlerFor(n int, fn func(context.Context, []string) error) any {
	switch n {
	case 0:
		return func(ctx context.Context) error { return fn(ctx, nil) }
	case 1:
		return func(ctx context.Context, a string) error { return fn(ctx, []string{a}) }
	case 2:
		return func(ctx context.Context, a, b string) error { return fn(ctx, []string{a, b}) }
	case 3:
		return func(ctx context.Context, a, b, c string) error { return fn(ctx, []string{a, b, c}) }
	case 4:
		return func(ctx context.Context, a, b, c, d string) error { return fn(ctx, []string{a, b, c, d}) }
	case 5:
		return func(ctx context.Context, a, b, c, d, e string) error { return fn(ctx, []string{a, b, c, d, e}) }
	default: // phrase.MaxParams

		return func(ctx context.Context, a, b, c, d, e, f string) error { return fn(ctx, []string{a, b, c, d, e, f}) }
	}
}

// Phrases lists every declared phrase with the request it runs, for `list`.
func Phrases(p *project.Project) map[string]*httpfile.Request {
	out := map[string]*httpfile.Request{}
	for _, req := range p.Requests() {
		for _, text := range req.Steps() {
			out[text] = req
		}
	}
	return out
}
