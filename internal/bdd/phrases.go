package bdd

import (
	"context"
	"fmt"

	"github.com/cucumber/godog"

	"github.com/dataGriff/api-caller/internal/httpfile"
	"github.com/dataGriff/api-caller/internal/phrase"
	"github.com/dataGriff/api-caller/internal/project"
)

// registerPhrases turns every `# @step` directive into a step that runs its
// request with the phrase's parameters as variables.
func registerPhrases(sc *godog.ScenarioContext, p *project.Project) error {
	for _, req := range p.Requests() {
		for _, text := range req.Steps() {
			ph, err := phrase.Parse(text)
			if err != nil {
				return fmt.Errorf("%s:%d: %w", req.File.Path, req.Line, err)
			}
			if len(ph.Params) > maxPhraseParams {
				return fmt.Errorf("%s:%d: @step %q has more than %d parameters", req.File.Path, req.Line, text, maxPhraseParams)
			}
			target := req.ID()
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
				return s.run(ctx, target, vars)
			}
			sc.Step(ph.Regex, handlerFor(len(ph.Params), run))
		}
	}
	return nil
}

const maxPhraseParams = 6

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
	default:
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
