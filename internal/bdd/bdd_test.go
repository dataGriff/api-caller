package bdd

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cucumber/godog"

	"github.com/dataGriff/api-caller/internal/project"
)

func server(t *testing.T) *httptest.Server {
	t.Helper()
	users := map[string]map[string]any{}
	next := 1
	mux := http.NewServeMux()
	mux.HandleFunc("POST /login", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"token":"t-1"}`))
	})
	mux.HandleFunc("POST /users", func(w http.ResponseWriter, r *http.Request) {
		var in map[string]any
		_ = json.NewDecoder(r.Body).Decode(&in)
		id := next
		next++
		in["id"] = id
		users[http.StatusText(id)] = in
		users[strings.TrimSpace(json.Number(itoa(id)).String())] = in
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(201)
		_ = json.NewEncoder(w).Encode(in)
	})
	mux.HandleFunc("GET /users/{id}", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer t-1" {
			w.WriteHeader(401)
			return
		}
		u, ok := users[r.PathValue("id")]
		w.Header().Set("Content-Type", "application/json")
		if !ok {
			w.WriteHeader(404)
			_, _ = w.Write([]byte(`{"error":"not found"}`))
			return
		}
		_ = json.NewEncoder(w).Encode(u)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func itoa(i int) string {
	return json.Number(strings.TrimSpace(strings.Repeat(" ", 0) + string(rune('0'+i)))).String()
}

const apiHTTP = `
### login
# @name login
# @step I am logged in
# @capture token = body.$.token
POST {{baseUrl}}/login

### create
# @name create-user
# @step a user named {name} exists
# @assert status == 201
# @capture userId = body.$.id
POST {{baseUrl}}/users
Content-Type: application/json

{"name": "{{name}}", "role": "{{role}}"}

### get
# @name get-user
# @step I fetch the user
# @step I fetch user {userId}
GET {{baseUrl}}/users/{{userId}}
Authorization: Bearer {{token}}
`

func newProject(t *testing.T, srv *httptest.Server) *project.Project {
	t.Helper()
	dir := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dir, "api.http"), []byte(apiHTTP), 0o644))
	must(t, os.WriteFile(filepath.Join(dir, "http-client.env.json"), []byte(`{"dev":{"baseUrl":"`+srv.URL+`","role":"member"},"other":{"baseUrl":"http://127.0.0.1:1"}}`), 0o644))
	p, err := project.Load(dir)
	must(t, err)
	return p
}

func run(t *testing.T, p *project.Project, feature string, env string) (*Summary, int) {
	t.Helper()
	var stderr bytes.Buffer
	sum, report, code, err := RunSummary(context.Background(), Options{
		Config:   Config{Project: p, Env: env, Stderr: &stderr},
		Features: []godog.Feature{{Name: "test.feature", Contents: []byte(feature)}},
	})
	if err != nil {
		t.Fatalf("run: %v\nreport: %s", err, report)
	}
	return sum, code
}

func TestVocabularyAndPhrases(t *testing.T) {
	srv := server(t)
	p := newProject(t, srv)
	sum, code := run(t, p, `
Feature: Users
  Background:
    Given I am logged in

  Scenario: Create and fetch
    Given a user named "alice" exists
    Then the response status is 201
    And the response is successful
    And the response body "$.name" is "alice"
    And the response body "name" starts with "al"
    And the response body "role" is "member"
    And the response header "content-type" contains "json"
    And the response body "$.id" exists
    And the response body "$.missing" does not exist
    And the response time is under 5000 ms
    And the response body contains:
      """
      {"name": "alice", "id": {{userId}}}
      """
    When I fetch the user
    Then the response status is 200
    And the response body "$.id" is "{{userId}}"
    And the response body is:
      """
      {"name": "alice", "role": "member", "id": {{userId}}}
      """

  Scenario: Variables and tables
    Given the variable "role" is "{{role}}-x"
    Then the variable "role" is "member-x"
    Given the variables:
      | name | value |
      | role | admin |
    When I run "create-user" with:
      | name | bob |
    Then the response body "role" is "admin"
    And the response body "$.name" is not "alice"
    When I capture the response body "$.id" as "bobId"
    And I fetch user {{bobId}}
    Then the response body "$.name" is "bob"

  Scenario Outline: Unknown users
    When I fetch user <id>
    Then the response status is 404
    And the response is a client error
    And the response body "$.error" matches "^not"
    Examples:
      | id  |
      | 0   |
      | 999 |
`, "dev")
	if code != ExitPassed || !sum.OK || sum.Scenarios != 4 || sum.Passed != 4 {
		t.Fatalf("code=%d summary=%+v", code, sum)
	}
}

func TestFailuresAndUndefined(t *testing.T) {
	srv := server(t)
	p := newProject(t, srv)
	sum, code := run(t, p, `
Feature: Failing
  Scenario: Assertion fails
    Given I am logged in
    When I fetch user 0
    Then the response status is 200

  Scenario: Request assert fails
    When I run "create-user" with:
      | name | x |
    Then the response status is 201

  Scenario: No response yet
    Then the response status is 200

  Scenario: Undefined step
    Given something apic does not know
`, "dev")
	if code != ExitFailed || sum.OK || sum.Failed != 3 || sum.Passed != 1 || sum.Undefined != 1 {
		t.Fatalf("code=%d summary=%+v", code, sum)
	}
	var msgs []string
	for _, f := range sum.Failures {
		msgs = append(msgs, f.Scenario+": "+f.Error)
	}
	joined := strings.Join(msgs, "\n")
	for _, want := range []string{"expected status == 200, got \"404\"", "no response yet"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in failures:\n%s", want, joined)
		}
	}
}

func TestIsolatedSessionAndEnvironmentStep(t *testing.T) {
	srv := server(t)
	p := newProject(t, srv)
	sum, code := run(t, p, `
Feature: Isolation
  Scenario: First logs in
    Given I am logged in
    When I fetch user 0
    Then the response status is 404

  Scenario: Second has no token from the first
    When I run "get-user" with:
      | userId | 0     |
      | token  | stale |
    Then the response status is 401
`, "dev")
	if code != ExitPassed || sum.Passed != 2 {
		t.Fatalf("code=%d summary=%+v", code, sum)
	}
	if _, err := os.Stat(filepath.Join(p.Root, ".apic")); !os.IsNotExist(err) {
		t.Fatal("tests must not write the session")
	}
	// Switching to an unreachable environment is a transport error (exit 3).
	_, _, code, err := RunSummary(context.Background(), Options{Config: Config{Project: p, Env: "dev"},
		Features: []godog.Feature{{Name: "s.feature", Contents: []byte("Feature: s\n  Scenario: switch\n    Given the environment is \"other\"\n    When I run \"login\"\n")}}})
	if code != ExitTransport || err == nil || !strings.Contains(err.Error(), "request failed") {
		t.Fatalf("environment switch: code=%d err=%v", code, err)
	}
}

func TestRedactHidesValuesInFailures(t *testing.T) {
	srv := server(t)
	p := newProject(t, srv)
	var stderr bytes.Buffer
	sum, _, code, err := RunSummary(context.Background(), Options{
		Config: Config{Project: p, Env: "dev", Redact: true, Stderr: &stderr},
		Features: []godog.Feature{{Name: "r.feature", Contents: []byte(`
Feature: Redacted
  Scenario: Compare
    Given I am logged in
    When a user named "secret-name" exists
    Then the response body "$.name" is "other-value"
  Scenario: Body match
    Given I am logged in
    When a user named "secret-name" exists
    Then the response body contains:
      """
      {"name": "expected-secret"}
      """
  Scenario: Request assert
    When I run "get-user" with:
      | userId | 0          |
      | token  | secret-tok |
    Then the response status is 200
`)}},
	})
	if err != nil || code != ExitFailed || sum.Failed != 3 {
		t.Fatalf("code=%d err=%v sum=%+v", code, err, sum)
	}
	for _, f := range sum.Failures {
		for _, leak := range []string{"secret-name", "other-value", "expected-secret", "secret-tok", "\"name\":"} {
			if strings.Contains(f.Error, leak) {
				t.Errorf("--redact leaked %q in %q", leak, f.Error)
			}
		}
	}
}

func TestEnvironmentStepUsageError(t *testing.T) {
	srv := server(t)
	p := newProject(t, srv)
	_, _, code, err := RunSummary(context.Background(), Options{Config: Config{Project: p, Env: "dev"},
		Features: []godog.Feature{{Name: "e.feature", Contents: []byte("Feature: e\n  Scenario: s\n    Given the environment is \"missing\"\n")}}})
	if code != ExitUsage || err == nil || !strings.Contains(err.Error(), `environment "missing"`) {
		t.Fatalf("code=%d err=%v", code, err)
	}
}

func TestEmptyFeatureDirIsUsageError(t *testing.T) {
	srv := server(t)
	p := newProject(t, srv)
	must(t, os.MkdirAll(filepath.Join(p.Root, "features"), 0o755))
	_, _, code, err := RunSummary(context.Background(), Options{Config: Config{Project: p, Env: "dev"}})
	if code != ExitUsage || err == nil || !strings.Contains(err.Error(), "no .feature files") {
		t.Fatalf("code=%d err=%v", code, err)
	}
}

func TestRunFileFlowStopsAtFailure(t *testing.T) {
	srv := server(t)
	p := newProject(t, srv)
	must(t, os.WriteFile(filepath.Join(p.Root, "flow.http"), []byte(`
### one
# @name flow-login
# @capture token = body.$.token
POST {{baseUrl}}/login

### two
# @name flow-missing
# @assert status == 200
GET {{baseUrl}}/users/0
Authorization: Bearer {{token}}

### three
# @name flow-never
GET {{baseUrl}}/users/0
Authorization: Bearer {{token}}
`), 0o644))
	p, err := project.Load(p.Root)
	must(t, err)
	sum, code := run(t, p, `
Feature: Files
  Scenario: A file runs in order and stops at the first failure
    When I run the file "flow.http"
  Scenario: The last response is the failing one
    When I run the file "flow.http"
    Then the response status is 404
`, "dev")
	if code != ExitFailed || sum.Failed != 2 || len(sum.Failures) != 2 {
		t.Fatalf("code=%d sum=%+v", code, sum)
	}
	for _, f := range sum.Failures {
		if !strings.Contains(f.Error, "flow-missing failed") || strings.Contains(f.Error, "flow-never") {
			t.Fatalf("flow should stop at flow-missing: %q", f.Error)
		}
	}
}

func TestTypedStepErrorsMapToExitCodes(t *testing.T) {
	srv := server(t)
	p := newProject(t, srv)
	_, _, code, err := RunSummary(context.Background(), Options{Config: Config{Project: p, Env: "dev"},
		Features: []godog.Feature{{Name: "u.feature", Contents: []byte("Feature: u\n  Scenario: s\n    When I run \"no-such-request\"\n")}}})
	if code != ExitUsage || err == nil || !strings.Contains(err.Error(), "no-such-request") {
		t.Fatalf("unknown request: code=%d err=%v", code, err)
	}
	_, _, code, err = RunSummary(context.Background(), Options{Config: Config{Project: p, Env: "other"},
		Features: []godog.Feature{{Name: "t.feature", Contents: []byte("Feature: t\n  Scenario: s\n    When I run \"login\"\n")}}})
	if code != ExitTransport || err == nil {
		t.Fatalf("unreachable server: code=%d err=%v", code, err)
	}
}

func TestRedactMasksSecretsInReportText(t *testing.T) {
	srv := server(t)
	dir := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dir, "api.http"), []byte(apiHTTP), 0o644))
	must(t, os.WriteFile(filepath.Join(dir, "http-client.env.json"), []byte(`{"dev":{"baseUrl":"`+srv.URL+`","role":"member"}}`), 0o644))
	must(t, os.WriteFile(filepath.Join(dir, "http-client.private.env.json"), []byte(`{"dev":{"apiKey":"hunter2-secret"}}`), 0o644))
	p, err := project.Load(dir)
	must(t, err)
	var report bytes.Buffer
	code, err := Run(context.Background(), Options{
		Config: Config{Project: p, Env: "dev", Redact: true},
		Format: "pretty", NoColors: true, Output: &report,
		Features: []godog.Feature{{Name: "m.feature", Contents: []byte(`
Feature: Masking
  Scenario: A captured token and a private value never appear in the report
    Given I am logged in
    When I run "get-user" with:
      | userId | 0              |
      | token  | hunter2-secret |
    Then the response status is 404
    And the response body "$.error" is "t-1"
`)}},
	})
	if err != nil || code != ExitFailed {
		t.Fatalf("code=%d err=%v", code, err)
	}
	for _, leak := range []string{"hunter2-secret", "t-1"} {
		if strings.Contains(report.String(), leak) {
			t.Errorf("--redact leaked %q:\n%s", leak, report.String())
		}
	}
	if !strings.Contains(report.String(), "***") {
		t.Fatalf("expected masked values in report:\n%s", report.String())
	}
}

func TestFeaturePathMustBeInsideProject(t *testing.T) {
	srv := server(t)
	p := newProject(t, srv)
	outside := t.TempDir()
	must(t, os.WriteFile(filepath.Join(outside, "x.feature"), []byte("Feature: x\n  Scenario: s\n    Given I am logged in\n"), 0o644))
	for _, path := range []string{filepath.Join(outside, "x.feature"), filepath.Join("..", filepath.Base(outside), "x.feature")} {
		_, _, code, err := RunSummary(context.Background(), Options{Config: Config{Project: p, Env: "dev"}, Paths: []string{path}})
		if code != ExitUsage || err == nil || !strings.Contains(err.Error(), "outside the project root") {
			t.Fatalf("%s: code=%d err=%v", path, code, err)
		}
	}
}

func TestEmptyTagSelectionIsNotAFailure(t *testing.T) {
	srv := server(t)
	p := newProject(t, srv)
	sum, _, code, err := RunSummary(context.Background(), Options{Config: Config{Project: p, Env: "dev"}, Tags: "@nothing",
		Features: []godog.Feature{{Name: "t.feature", Contents: []byte("Feature: t\n  Scenario: s\n    Given I am logged in\n")}}})
	if err != nil || code != ExitPassed || !sum.OK || sum.Scenarios != 0 {
		t.Fatalf("code=%d err=%v sum=%+v", code, err, sum)
	}
}

func TestUsageErrors(t *testing.T) {
	srv := server(t)
	p := newProject(t, srv)
	_, _, code, err := RunSummary(context.Background(), Options{Config: Config{Project: p, Env: "nope"},
		Features: []godog.Feature{{Name: "x.feature", Contents: []byte("Feature: x\n  Scenario: y\n    Given I am logged in\n")}}})
	if code != ExitUsage || err == nil || !strings.Contains(err.Error(), `environment "nope"`) {
		t.Fatalf("code=%d err=%v", code, err)
	}
	_, _, code, err = RunSummary(context.Background(), Options{Config: Config{Project: p, Env: "dev"}, Paths: []string{"nowhere"}})
	if code != ExitUsage || err == nil || !strings.Contains(err.Error(), "no features") {
		t.Fatalf("code=%d err=%v", code, err)
	}
}

func TestJSONMatch(t *testing.T) {
	ok, why := jsonContains([]byte(`{"a":1,"b":{"c":[1,2],"d":"x"},"extra":true}`), []byte(`{"b":{"c":[1,2]}}`))
	if !ok {
		t.Fatal(why)
	}
	if ok, why = jsonContains([]byte(`{"a":1}`), []byte(`{"a":2}`)); ok || !strings.Contains(why, "$.a: expected 2, got 1") {
		t.Fatal(why)
	}
	if ok, why = jsonEqual([]byte(`{"a":1,"b":2}`), []byte(`{"a":1}`)); ok || !strings.Contains(why, "unexpected key") {
		t.Fatal(why)
	}
	if ok, _ = jsonEqual([]byte(` {"b": 2, "a": [1, {"x": null}]} `), []byte(`{"a":[1,{"x":null}],"b":2}`)); !ok {
		t.Fatal("semantic equality should ignore order and whitespace")
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestRedactCaptureStepAndShortValues(t *testing.T) {
	srv := server(t)
	dir := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dir, "api.http"), []byte(apiHTTP), 0o644))
	must(t, os.WriteFile(filepath.Join(dir, "http-client.env.json"), []byte(`{"dev":{"baseUrl":"`+srv.URL+`","role":"member"}}`), 0o644))
	must(t, os.WriteFile(filepath.Join(dir, "http-client.private.env.json"), []byte(`{"dev":{"pin":"7"}}`), 0o644))
	p, err := project.Load(dir)
	must(t, err)
	var report bytes.Buffer
	_, err = Run(context.Background(), Options{
		Config: Config{Project: p, Env: "dev", Redact: true},
		Format: "pretty", NoColors: true, Output: &report,
		Features: []godog.Feature{{Name: "m.feature", Contents: []byte(`
Feature: Capture step
  Scenario: A value captured by the capture step is masked afterwards
    Given I am logged in
    And a user named "alice" exists
    When I capture the response body "$.name" as "who"
    Then the response body "$.name" is "alice"
    And the response body "$.name" is "bob"
`)}},
	})
	must(t, err)
	out := report.String()
	if !strings.Contains(out, `Then the response body "$.name" is "***"`) {
		t.Errorf("value captured by the capture step should be masked in later lines:\n%s", out)
	}
	// A one-character secret is deliberately not masked by substitution: it
	// would corrupt counts, line numbers and JSON in the report.
	if !strings.Contains(out, "1 scenarios (1 failed)") || !strings.Contains(out, "m.feature:3") {
		t.Errorf("report structure must survive short secrets:\n%s", out)
	}
}

func TestSymlinkedFeatureOutsideProjectRejected(t *testing.T) {
	srv := server(t)
	p := newProject(t, srv)
	outside := t.TempDir()
	must(t, os.WriteFile(filepath.Join(outside, "leak.feature"), []byte("Feature: x\n  Scenario: s\n    Given I am logged in\n"), 0o644))
	must(t, os.MkdirAll(filepath.Join(p.Root, "features"), 0o755))
	if err := os.Symlink(filepath.Join(outside, "leak.feature"), filepath.Join(p.Root, "features", "leak.feature")); err != nil {
		t.Skip("symlinks not supported here")
	}
	_, _, code, err := RunSummary(context.Background(), Options{Config: Config{Project: p, Env: "dev"}})
	if code != ExitUsage || err == nil || !strings.Contains(err.Error(), "outside the project root") {
		t.Fatalf("code=%d err=%v", code, err)
	}
}

func TestMissingVariableInStepArgumentIsUsageError(t *testing.T) {
	srv := server(t)
	p := newProject(t, srv)
	_, _, code, err := RunSummary(context.Background(), Options{Config: Config{Project: p, Env: "dev"},
		Features: []godog.Feature{{Name: "v.feature", Contents: []byte("Feature: v\n  Scenario: s\n    Given I am logged in\n    Then the response body \"$.token\" is \"{{nope}}\"\n")}}})
	if code != ExitUsage || err == nil || !strings.Contains(err.Error(), "{{nope}}") {
		t.Fatalf("code=%d err=%v", code, err)
	}
}

func TestPhraseConflictingWithBuiltinIsUsageError(t *testing.T) {
	srv := server(t)
	dir := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dir, "api.http"), []byte("### a\n# @name a\n# @step I run {thing}\nGET {{baseUrl}}/login\n"), 0o644))
	must(t, os.WriteFile(filepath.Join(dir, "http-client.env.json"), []byte(`{"dev":{"baseUrl":"`+srv.URL+`"}}`), 0o644))
	p, err := project.Load(dir)
	must(t, err)
	_, _, code, err := RunSummary(context.Background(), Options{Config: Config{Project: p, Env: "dev"},
		Features: []godog.Feature{{Name: "c.feature", Contents: []byte("Feature: c\n  Scenario: s\n    When I run \"login\"\n")}}})
	if code != ExitUsage || err == nil || !strings.Contains(err.Error(), "the built-in step") {
		t.Fatalf("code=%d err=%v", code, err)
	}
}

func TestPhraseTargetsSurviveDuplicateNames(t *testing.T) {
	srv := server(t)
	dir := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dir, "a.http"), []byte("### a\n# @name login\n# @step I log in via a\n# @capture token = body.$.token\nPOST {{baseUrl}}/login\n"), 0o644))
	must(t, os.WriteFile(filepath.Join(dir, "b.http"), []byte("### b\n# @name login\n# @step I log in via b\nPOST {{baseUrl}}/login\n"), 0o644))
	must(t, os.WriteFile(filepath.Join(dir, "http-client.env.json"), []byte(`{"dev":{"baseUrl":"`+srv.URL+`"}}`), 0o644))
	p, err := project.Load(dir)
	must(t, err)
	sum, code := run(t, p, "Feature: d\n  Scenario: s\n    Given I log in via a\n    And I log in via b\n    Then the response status is 200\n", "dev")
	if code != ExitPassed || !sum.OK {
		t.Fatalf("code=%d sum=%+v", code, sum)
	}
}

func TestEnvironmentStepRendersVariables(t *testing.T) {
	srv := server(t)
	p := newProject(t, srv)
	sum, code := run(t, p, "Feature: e\n  Scenario: s\n    Given the variable \"target\" is \"dev\"\n    And the environment is \"{{target}}\"\n    When I run \"login\"\n    Then the response status is 200\n", "dev")
	if code != ExitPassed || !sum.OK {
		t.Fatalf("code=%d sum=%+v", code, sum)
	}
}

func TestRedactedTransportErrorIsRecorded(t *testing.T) {
	srv := server(t)
	dir := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dir, "api.http"), []byte("### a\n# @name ping\nGET {{baseUrl}}/ping?token={{secret}}\n"), 0o644))
	must(t, os.WriteFile(filepath.Join(dir, "http-client.env.json"), []byte(`{"dev":{"baseUrl":"http://127.0.0.1:1"}}`), 0o644))
	must(t, os.WriteFile(filepath.Join(dir, "http-client.private.env.json"), []byte(`{"dev":{"secret":"hunter2-value"}}`), 0o644))
	_ = srv
	p, err := project.Load(dir)
	must(t, err)
	sum, _, code, err := RunSummary(context.Background(), Options{Config: Config{Project: p, Env: "dev", Redact: true},
		Features: []godog.Feature{{Name: "t.feature", Contents: []byte("Feature: t\n  Scenario: s\n    When I run \"ping\"\n")}}})
	if code != ExitTransport || err == nil || strings.Contains(err.Error(), "hunter2-value") || !strings.Contains(err.Error(), "token=***") {
		t.Fatalf("code=%d err=%v", code, err)
	}
	if sum == nil || sum.Scenarios != 1 || sum.Failed != 1 {
		t.Fatalf("summary should still describe the run: %+v", sum)
	}
}

func TestRedactCucumberJSONStaysValidWithNumericSecret(t *testing.T) {
	srv := server(t)
	dir := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dir, "api.http"), []byte(apiHTTP), 0o644))
	must(t, os.WriteFile(filepath.Join(dir, "http-client.env.json"), []byte(`{"dev":{"baseUrl":"`+srv.URL+`","role":"member"}}`), 0o644))
	must(t, os.WriteFile(filepath.Join(dir, "http-client.private.env.json"), []byte(`{"dev":{"pin":"123"}}`), 0o644))
	t.Setenv("APIC_VAR_shellSecret", "from-shell-secret")
	p, err := project.Load(dir)
	must(t, err)
	sum, report, code, err := RunSummary(context.Background(), Options{
		Config: Config{Project: p, Env: "dev", Redact: true},
		Features: []godog.Feature{{Name: "n.feature", Contents: []byte(`
Feature: Numbers
  Scenario: A numeric secret and a shell secret
    Given I am logged in
    When I run "get-user" with:
      | userId | 123               |
      | token  | from-shell-secret |
    Then the response status is 123
`)}},
	})
	if err != nil || code != ExitFailed {
		t.Fatalf("code=%d err=%v report=%s", code, err, report)
	}
	if sum == nil || sum.Scenarios != 1 || sum.Failed != 1 {
		t.Fatalf("cucumber JSON must stay parseable under --redact: %+v\n%s", sum, report)
	}
	for _, leak := range []string{"from-shell-secret", "| userId | 123"} {
		if strings.Contains(string(report), leak) {
			t.Errorf("--redact leaked %q:\n%s", leak, report)
		}
	}
	if !strings.Contains(string(report), `"line": 3`) {
		t.Errorf("numbers in the report must survive masking:\n%s", report)
	}
}

func TestRunTargetRendersVariablesAndStatusNot(t *testing.T) {
	srv := server(t)
	p := newProject(t, srv)
	sum, code := run(t, p, `
Feature: Targets
  Scenario: The request id can come from a variable
    Given the variable "which" is "login"
    When I run "{{which}}"
    Then the response status is not 500
    And the response status is 200
    When I run "{{which}}" with:
      | role | admin |
    Then the response is successful
`, "dev")
	if code != ExitPassed || !sum.OK {
		t.Fatalf("code=%d sum=%+v", code, sum)
	}
}

func TestCaptureStepPersistsWithUseSession(t *testing.T) {
	srv := server(t)
	p := newProject(t, srv)
	var stderr bytes.Buffer
	sum, _, code, err := RunSummary(context.Background(), Options{
		Config: Config{Project: p, Env: "dev", UseSession: true, Stderr: &stderr},
		Features: []godog.Feature{{Name: "s.feature", Contents: []byte(`
Feature: Session
  Scenario: Capture into the shared session
    Given I am logged in
    When I capture the response body "$.token" as "kept"
  Scenario: A later scenario sees it
    When I run "get-user" with:
      | userId | 0        |
      | token  | {{kept}} |
    Then the response status is 404
`)}},
	})
	if err != nil || code != ExitPassed || !sum.OK {
		t.Fatalf("code=%d err=%v sum=%+v", code, err, sum)
	}
	data, err := os.ReadFile(filepath.Join(p.Root, ".apic", "session.json"))
	must(t, err)
	if !strings.Contains(string(data), `"kept": "t-1"`) {
		t.Fatalf("capture step should persist under --use-session: %s", data)
	}
}

func TestCaptureStepUsesCaptureLayer(t *testing.T) {
	srv := server(t)
	p := newProject(t, srv)
	sum, _, code, err := RunSummary(context.Background(), Options{
		Config: Config{Project: p, Env: "dev", Vars: map[string]string{"pinned": "from-var"}},
		Features: []godog.Feature{{Name: "c.feature", Contents: []byte(`
Feature: Capture precedence
  Scenario: A capture step cannot override --var, and a later request capture replaces it
    Given I am logged in
    When I capture the response body "$.token" as "pinned"
    Then the variable "check" is "{{pinned}}"
    And the variable "check" is "from-var"
    When I capture the response body "$.token" as "token"
    And I run "get-user" with:
      | userId | 0 |
    Then the response status is 404
`)}},
	})
	if err != nil || code != ExitPassed || !sum.OK {
		t.Fatalf("code=%d err=%v sum=%+v", code, err, sum)
	}
}

func TestRedactRegistersSecretsForDefaultEnv(t *testing.T) {
	srv := server(t)
	dir := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dir, "api.http"), []byte(apiHTTP), 0o644))
	must(t, os.WriteFile(filepath.Join(dir, "apic.yaml"), []byte("env: dev\n"), 0o644))
	must(t, os.WriteFile(filepath.Join(dir, "http-client.env.json"), []byte(`{"dev":{"baseUrl":"`+srv.URL+`","role":"member"}}`), 0o644))
	must(t, os.WriteFile(filepath.Join(dir, "http-client.private.env.json"), []byte(`{"dev":{"apiKey":"hunter2-default-env"}}`), 0o644))
	p, err := project.Load(dir)
	must(t, err)
	var report bytes.Buffer
	_, err = Run(context.Background(), Options{
		Config: Config{Project: p, Redact: true}, // Env left empty: apic.yaml supplies it
		Format: "pretty", NoColors: true, Output: &report,
		Features: []godog.Feature{{Name: "d.feature", Contents: []byte(`
Feature: Default env
  Scenario: Private values of the configured default environment are masked
    Given I am logged in
    When I run "get-user" with:
      | userId | 0                   |
      | token  | hunter2-default-env |
    Then the response status is 404
`)}},
	})
	must(t, err)
	if strings.Contains(report.String(), "hunter2-default-env") {
		t.Fatalf("private value of the default environment leaked:\n%s", report.String())
	}
}

func TestEmptyOrInvalidVariableNamesAreRejected(t *testing.T) {
	srv := server(t)
	p := newProject(t, srv)
	for _, feature := range []string{
		"Feature: n\n  Scenario: s\n    Given the variable \"\" is \"x\"\n",
		"Feature: n\n  Scenario: s\n    Given the variables:\n      | | x |\n",
		"Feature: n\n  Scenario: s\n    Given I am logged in\n    When I capture the response body \"$.token\" as \"\"\n",
		"Feature: n\n  Scenario: s\n    Given the variable \"bad name\" is \"x\"\n",
	} {
		_, _, code, err := RunSummary(context.Background(), Options{Config: Config{Project: p, Env: "dev"},
			Features: []godog.Feature{{Name: "n.feature", Contents: []byte(feature)}}})
		if code == ExitPassed {
			t.Errorf("should not pass:\n%s (code=%d err=%v)", feature, code, err)
		}
	}
}
