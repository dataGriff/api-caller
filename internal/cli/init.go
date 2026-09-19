package cli

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dataGriff/api-caller/internal/runner"
)

func (a *App) initCmd() *cobra.Command {
	var baseURL, envName string
	var force bool
	cmd := &cobra.Command{
		Use:   "init [dir]",
		Short: "Scaffold a new apic project: config, env files and a first request",
		Long: `init writes a small, working project into dir (default: the current
directory): apic.yaml, http-client.env.json, a gitignored
http-client.private.env.json, api.http with one annotated request, and a
features/smoke.feature to run with apic test. Existing files are left alone
unless --force is given.`,
		Example: `  apic init
  apic init api --base-url https://dev.example.com --env dev
  apic init --json`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := "."
			if len(args) == 1 {
				dir = args[0]
			}
			if envName == "" {
				return &runner.UsageError{Msg: "--env must not be empty"}
			}
			written, skipped, err := writeInitProject(dir, baseURL, envName, force)
			if err != nil {
				return err
			}
			if a.g.json {
				return a.writeJSON(struct {
					Out     string   `json:"out"`
					Env     string   `json:"env"`
					Written []string `json:"written"`
					Skipped []string `json:"skipped,omitempty"`
				}{dir, envName, written, skipped})
			}
			for _, f := range written {
				fmt.Fprintf(a.Stdout, "%s %s\n", theme.OK.Render("wrote"), f)
			}
			for _, f := range skipped {
				fmt.Fprintf(a.Stdout, "%s  %s %s\n", theme.Warn.Render("kept"), f, theme.Dim.Render("(use --force to overwrite)"))
			}
			fmt.Fprintf(a.Stdout, "\n%s\n", theme.Bold.Render("next:"))
			if dir != "." {
				fmt.Fprintf(a.Stdout, "  cd %s\n", dir)
			}
			fmt.Fprintf(a.Stdout, "  edit api.http, then:  %s\n", theme.Dim.Render("apic list · apic run ping · apic ui"))
			return nil
		},
	}
	cmd.Flags().StringVar(&baseURL, "base-url", "https://api.example.com", "baseUrl for the environment")
	cmd.Flags().StringVar(&envName, "env", "dev", "name of the first environment")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite existing files")
	return cmd
}

// schemaModeline is the first line of a scaffolded apic.yaml: the YAML
// language server (and so VS Code, JetBrains and Neovim with it) picks up
// the published schema for completion and validation.
const schemaModeline = "# yaml-language-server: $schema=https://datagriff.github.io/api-caller/schemas/apic.schema.json"

// writeInitProject writes the starter files, skipping ones that exist unless
// force is set. The .gitignore is appended to rather than replaced.
func writeInitProject(dir, baseURL, envName string, force bool) (written, skipped []string, err error) {
	if err := os.MkdirAll(filepath.Join(dir, "features"), 0o755); err != nil { //nolint:gosec // a scaffolded project directory the user browses and edits
		return nil, nil, err
	}
	write := func(name, content string, mode fs.FileMode) error {
		target := filepath.Join(dir, name)
		if !force {
			if _, err := os.Stat(target); err == nil {
				skipped = append(skipped, target)
				return nil
			}
		}
		if err := os.WriteFile(target, []byte(content), mode); err != nil {
			return err
		}
		written = append(written, target)
		return nil
	}
	files := []struct {
		name, content string
		mode          fs.FileMode
	}{
		{"apic.yaml", fmt.Sprintf("%s\n# Default environment when --env is not given.\nenv: %s\n", schemaModeline, envName), 0o644},
		{"http-client.env.json", fmt.Sprintf("{\n  \"$shared\": {},\n  %q: {\n    \"baseUrl\": %q\n  }\n}\n", envName, baseURL), 0o644},
		{"http-client.private.env.json", fmt.Sprintf("{\n  %q: {\n    \"apiKey\": \"change-me\"\n  }\n}\n", envName), 0o600},
		{"api.http", initHTTP, 0o644},
		{filepath.Join("features", "smoke.feature"), initFeature, 0o644},
	}
	for _, f := range files {
		if err := write(f.name, f.content, f.mode); err != nil {
			return nil, nil, err
		}
	}
	if err := ensureGitignore(dir, &written); err != nil {
		return nil, nil, err
	}
	return written, skipped, nil
}

func ensureGitignore(dir string, written *[]string) error {
	target := filepath.Join(dir, ".gitignore")
	// .env belongs here too: env.Load reads it and vars.go marks its values
	// secret, so it is a credential file like the private env file.
	lines := []string{"http-client.private.env.json", ".env", ".apic/"}
	existing, err := os.ReadFile(target) //nolint:gosec // the project's own .gitignore, in the directory the user named
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	var add []string
	for _, l := range lines {
		if !strings.Contains(string(existing), l) {
			add = append(add, l)
		}
	}
	if len(add) == 0 {
		return nil
	}
	content := string(existing)
	if content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	content += strings.Join(add, "\n") + "\n"
	// Non-secret scaffolding. The private env file is written 0600 above.
	if err := os.WriteFile(target, []byte(content), 0o644); err != nil { //nolint:gosec // public project file the user edits and commits
		return err
	}
	*written = append(*written, target)
	return nil
}

const initHTTP = `### Is the API up?
# A request apic can run right away: apic run ping
# @name ping
# @step the API is up
# @assert status == 200
GET {{baseUrl}}/health
Accept: application/json

### Fetch something that needs the key from http-client.private.env.json
# @name get-thing
# @description Fetch one thing by id
# @step I fetch thing {id}
# @assert status == 200
# @capture thingName = body.$.name
GET {{baseUrl}}/things/{{id}}
X-Api-Key: {{apiKey}}
Accept: application/json
`

const initFeature = `Feature: Smoke
  The requests live in api.http; the phrases used here are declared on
  them with "# @step".

  Scenario: The API answers
    When the API is up
    Then the response status is 200
`
