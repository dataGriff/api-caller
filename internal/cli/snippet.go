// apic snippet: a request as code in another language.

package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dataGriff/api-caller/internal/runner"
	"github.com/dataGriff/api-caller/internal/snippet"
)

func (a *App) snippetCmd() *cobra.Command {
	var lang string
	cmd := &cobra.Command{
		Use:   "snippet <request>",
		Short: "Print the request as code: curl, HTTPie, PowerShell, Python, JavaScript or Go",
		Long: `Print code that sends what apic would, variables resolved: a curl or
HTTPie command, PowerShell's Invoke-RestMethod, Python requests, JavaScript
fetch or a Go program using net/http.

Without --redact the snippet runs as printed, credentials included. With it
the values are masked and credentials come from the environment: TOKEN for a
bearer or OAuth2 token, APIC_USER and APIC_PASSWORD for basic and digest,
APIC_API_KEY for an API key. What a language cannot do on its own, such as
AWS SigV4 signing, is said in a comment at the top.`,
		Example: `  apic snippet get-user --lang python
  apic snippet create-order --lang powershell --redact
  apic snippet login --lang go > login.go`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, code, err := a.snippet(args[0], lang)
			if err != nil {
				return err
			}
			if a.g.json {
				return a.writeJSON(struct {
					ID   string `json:"id"`
					Lang string `json:"lang"`
					Code string `json:"code"`
				}{id, lang, code})
			}
			fmt.Fprintln(a.Stdout, code)
			return nil
		},
	}
	cmd.ValidArgsFunction = a.completeRequests
	cmd.Flags().StringVarP(&lang, "lang", "l", "curl", "language: "+strings.Join(snippet.Languages, ", "))
	_ = cmd.RegisterFlagCompletionFunc("lang", func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		return snippet.Languages, cobra.ShellCompDirectiveNoFileComp
	})
	return cmd
}

// snippet resolves a request and renders it in lang, refusing when a
// variable is missing: code with a hole in it would not send the request.
func (a *App) snippet(target, lang string) (id, code string, err error) {
	known := false
	for _, l := range snippet.Languages {
		known = known || l == lang
	}
	if !known {
		return "", "", &runner.UsageError{Msg: fmt.Sprintf("unknown language %q: one of %s", lang, strings.Join(snippet.Languages, ", "))}
	}
	r, req, err := a.single(target)
	if err != nil {
		return "", "", err
	}
	res, err := r.Resolve(req)
	if err != nil {
		return "", "", err
	}
	if d := r.Describe(req); !d.Ready {
		var missing []string
		for _, v := range d.Variables {
			if v.Missing {
				missing = append(missing, v.Name)
			}
		}
		return "", "", r.MissingError(req, missing)
	}
	code, err = snippet.Render(lang, res, a.g.redact)
	if err != nil {
		return "", "", err
	}
	return req.ID(), code, nil
}
