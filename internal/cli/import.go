package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dataGriff/api-caller/internal/curlimport"
	"github.com/dataGriff/api-caller/internal/httpfile"
	"github.com/dataGriff/api-caller/internal/openapi"
	"github.com/dataGriff/api-caller/internal/postman"
	"github.com/dataGriff/api-caller/internal/runner"
)

func (a *App) importCmd() *cobra.Command {
	var out, envName, curl, into, name string
	var force bool
	var postmanEnvs []string
	cmd := &cobra.Command{
		Use:   "import <openapi.yaml|openapi.json|collection.postman.json> | --curl <command>",
		Short: "Scaffold .http files from an OpenAPI 3 document, a Postman collection or a curl command",
		Long: `Generate .http files from an OpenAPI 3 document (one file per tag, a named
request per operation, example bodies from the schemas, an env file with the
server URL) or from a Postman v2.1 collection (folders become files, requests
become named requests, variables become env files, and the simple pm.test
checks become # @assert and # @capture lines). The format is detected from
the file. Existing files are left alone unless --force is given.

--curl turns one curl command into a request block: printed, or appended
to the file --into names (created if needed). --curl - reads the command
from stdin, so a copied command can be piped in.`,
		Example: `  apic import openapi.yaml -o api --env-name dev
  apic import collection.postman.json -o api --postman-env staging.postman_environment.json
  apic import --curl 'curl -X POST https://api.example.com/todos -H "Content-Type: application/json" -d "{}"' --into todos.http --name create-todo
  pbpaste | apic import --curl - --into todos.http`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if curl != "" {
				if len(args) > 0 {
					return runner.Usage(runner.CodeFlag, "--curl takes the command, not a file")
				}
				return a.importCurl(curl, into, name)
			}
			if len(args) != 1 {
				return runner.Usage(runner.CodeFlag, "import needs a file to read, or --curl")
			}
			if into != "" || name != "" {
				return runner.Usage(runner.CodeFlag, "--into and --name go with --curl")
			}
			data, err := os.ReadFile(args[0]) //nolint:gosec // the file the user named on the command line
			if err != nil {
				return runner.Usage(runner.CodeFile, err.Error())
			}
			if postman.LooksLikeCollection(data) {
				res, err := postman.Import(args[0], postman.Options{OutDir: out, EnvFiles: postmanEnvs, Force: force})
				if err != nil {
					return runner.Usage(runner.CodeImport, err.Error())
				}
				if a.g.json {
					return a.writeJSON(res)
				}
				a.printImport(res.Files, res.Skipped, res.EnvFile, res.BaseURL, res.Requests)
				for _, u := range res.Unsupported {
					fmt.Fprintf(a.Stdout, "note  %s: %s: %s\n", u.Request, u.What, u.Reason)
				}
				return nil
			}
			if postman.LooksLikeEnvironment(data) {
				return runner.Usage(runner.CodeFlag, args[0]+" is a Postman environment; pass the collection and give environments with --postman-env")
			}
			if len(postmanEnvs) > 0 {
				return runner.Usage(runner.CodeFlag, "--postman-env goes with a Postman collection, not an OpenAPI document")
			}
			res, err := openapi.Import(args[0], openapi.Options{OutDir: out, EnvName: envName, Force: force})
			if err != nil {
				return runner.Usage(runner.CodeImport, err.Error())
			}
			if a.g.json {
				return a.writeJSON(res)
			}
			a.printImport(res.Files, res.Skipped, res.EnvFile, res.BaseURL, res.Requests)
			return nil
		},
	}
	cmd.Flags().StringVarP(&out, "out", "o", ".", "directory to write .http files into")
	cmd.Flags().StringVar(&envName, "env-name", "dev", "environment name for the generated http-client.env.json (OpenAPI)")
	cmd.Flags().StringArrayVar(&postmanEnvs, "postman-env", nil, "Postman environment export to turn into an environment (repeatable)")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite existing files")
	cmd.Flags().StringVar(&curl, "curl", "", "a curl command to turn into a request (- reads stdin)")
	cmd.Flags().StringVar(&into, "into", "", "append the request to this .http file (relative to the project root), creating it if needed")
	cmd.Flags().StringVar(&name, "name", "", "request name (default: from the method and path)")
	return cmd
}

// importCurl turns a curl command into a request block.
func (a *App) importCurl(command, into, name string) error {
	if command == "-" {
		data, err := io.ReadAll(a.Stdin)
		if err != nil {
			return runner.Usage(runner.CodeFile, "reading the command from stdin: "+err.Error())
		}
		command = string(data)
	}
	req, err := curlimport.Parse(command)
	if err != nil {
		return runner.Usage(runner.CodeImport, "curl: "+err.Error())
	}
	// The project's baseUrl, when there is one, replaces the host.
	baseURL := ""
	if r, err := a.newRunner(); err == nil {
		for _, v := range r.EnvVars() {
			if v.Name == "baseUrl" && !v.Missing {
				baseURL = v.Value
			}
		}
	}
	if name == "" {
		name = curlimport.Name(req)
	}
	var target string
	existing := ""
	if into != "" {
		target = into
		if !filepath.IsAbs(target) {
			target = filepath.Join(a.g.dir, into)
		}
		data, err := os.ReadFile(target) //nolint:gosec // the request file the user named
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return runner.Usage(runner.CodeImport, err.Error())
		}
		existing = string(data)
		// A name already in the file gets a suffix rather than a duplicate.
		f, _ := httpfile.Parse(into, existing)
		taken := map[string]bool{}
		for _, r := range f.Requests {
			taken[r.Name] = true
		}
		base := name
		for i := 2; taken[name]; i++ {
			name = fmt.Sprintf("%s-%d", base, i)
		}
	}
	if req.Body != "" && curlimport.SplitsBlock(req.Body) {
		// The parser would read this body as a new block or a file
		// reference, so it goes into a side file the request points at.
		if target == "" {
			req.Warnings = append(req.Warnings, "the body starts a new block or looks like a file reference as written; use --into so it can go into a side file")
		} else {
			side := name + ".body.txt"
			sidePath := filepath.Join(filepath.Dir(target), side)
			if err := os.WriteFile(sidePath, []byte(req.Body), 0o644); err != nil { //nolint:gosec // a request body file the user edits and commits
				return runner.Usage(runner.CodeImport, err.Error())
			}
			req.Body, req.BodyFile = "", "./"+side
			fmt.Fprintf(a.Stdout, "wrote %s\n", filepath.Join(filepath.Dir(into), side))
		}
	}
	block := curlimport.Render(req, name, baseURL)
	if target != "" {
		text := block
		if existing != "" {
			text = strings.TrimRight(existing, "\n") + "\n\n" + block
		}
		if err := os.WriteFile(target, []byte(text), 0o644); err != nil { //nolint:gosec // a .http file the user edits and commits
			return runner.Usage(runner.CodeImport, err.Error())
		}
	}
	if a.g.json {
		return a.writeJSON(struct {
			File     string   `json:"file,omitempty"`
			Name     string   `json:"name"`
			Request  string   `json:"request"`
			Warnings []string `json:"warnings"`
		}{into, name, block, nonNil(req.Warnings)})
	}
	if target != "" {
		fmt.Fprintf(a.Stdout, "added %s to %s\n", name, into)
	} else {
		fmt.Fprint(a.Stdout, block)
	}
	for _, w := range req.Warnings {
		fmt.Fprintf(a.Stderr, "note: %s\n", w)
	}
	return nil
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func (a *App) printImport(files, skipped []string, envFile, baseURL string, requests int) {
	for _, f := range files {
		if f == envFile {
			continue
		}
		fmt.Fprintf(a.Stdout, "wrote %s\n", f)
	}
	for _, f := range skipped {
		fmt.Fprintf(a.Stdout, "kept  %s (use --force to overwrite)\n", f)
	}
	if envFile != "" {
		if baseURL != "" {
			fmt.Fprintf(a.Stdout, "wrote %s (baseUrl = %s)\n", envFile, baseURL)
		} else {
			fmt.Fprintf(a.Stdout, "wrote %s\n", envFile)
		}
	}
	fmt.Fprintf(a.Stdout, "%d request(s) generated; run `apic list` to see them\n", requests)
}
