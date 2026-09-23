package bdd

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
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
	mux.HandleFunc("POST /login-form", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "sid", Value: "c-1", Path: "/"})
		w.WriteHeader(204)
	})
	mux.HandleFunc("GET /me", func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie("sid"); err != nil || c.Value != "c-1" {
			w.WriteHeader(401)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"user":"alice"}`))
	})
	mux.HandleFunc("POST /users", func(w http.ResponseWriter, r *http.Request) {
		var in map[string]any
		_ = json.NewDecoder(r.Body).Decode(&in)
		id := next
		next++
		in["id"] = id
		users[strconv.Itoa(id)] = in
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
	mux.HandleFunc("GET /catalog", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Add("Link", "<https://x/catalog?page=2>; rel=next")
		w.Header().Add("Link", "<https://x/catalog?page=9>; rel=last")
		_, _ = w.Write([]byte(`{"items": [{"id": "a", "name": "apple", "done": true, "price": 1.5}, {"id": "b", "name": "banana", "done": false, "price": 0.25}, {"id": "c", "name": "avocado", "done": true, "price": 2}], "owner": {"id": "u1", "tags": []}, "note": null}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
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

### get, logging in by itself
# @name get-user-ref
# @ref login
# @step I fetch user {userId} logged in
GET {{baseUrl}}/users/{{userId}}
Authorization: Bearer {{token}}

### a form login answered with a session cookie
# @name cookie-login
# @step I log in with a form
POST {{baseUrl}}/login-form

### only a cookie gets in
# @name me
# @step I ask who I am
GET {{baseUrl}}/me

### a list to select from
# @name catalog
# @step I list the catalog
GET {{baseUrl}}/catalog
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

func TestScopedVarsBackgroundFailuresAndMaskedErrors(t *testing.T) {
	srv := server(t)
	p := newProject(t, srv)
	// Phrase and table variables do not leak into later steps; a table may supply the target.
	sum, code := run(t, p, `
Feature: Scoping
  Scenario: Phrase parameters are scoped to their request
    Given I am logged in
    And a user named "alice" exists
    Then the variable "probe" is "{{role}}"
    And the variable "probe" is "member"
    When I run "{{which}}" with:
      | which | login |
    Then the response status is 200
`, "dev")
	if code != ExitPassed || !sum.OK {
		t.Fatalf("code=%d sum=%+v", code, sum)
	}
	// A failing Background marks every scenario failed in the summary.
	sum, code = run(t, p, `
Feature: Background failure
  Background:
    Given I am logged in
    And the response status is 500
  Scenario: one
    Then the response status is 200
  Scenario: two
    Then the response status is 200
`, "dev")
	if code != ExitFailed || sum.Failed != 2 || sum.Passed != 0 || len(sum.Failures) != 2 || sum.Failures[0].Scenario != "one" {
		t.Fatalf("background failure not attributed: code=%d sum=%+v", code, sum)
	}
	// Table errors and unknown-request errors are usage errors, masked under --redact.
	_, _, code, err := RunSummary(context.Background(), Options{Config: Config{Project: p, Env: "dev", Redact: true, Vars: map[string]string{"secretTarget": "hunter2-target"}},
		Features: []godog.Feature{{Name: "m.feature", Contents: []byte("Feature: m\n  Scenario: s\n    When I run \"{{secretTarget}}\"\n")}}})
	if code != ExitUsage || err == nil || strings.Contains(err.Error(), "hunter2-target") || !strings.Contains(err.Error(), "***") {
		t.Fatalf("resolve error must be masked: code=%d err=%v", code, err)
	}
	_, _, code, err = RunSummary(context.Background(), Options{Config: Config{Project: p, Env: "dev", Redact: true, Vars: map[string]string{"secretEnv": "hunter2-env"}},
		Features: []godog.Feature{{Name: "e.feature", Contents: []byte("Feature: e\n  Scenario: s\n    Given the environment is \"{{secretEnv}}\"\n")}}})
	if code != ExitUsage || err == nil || strings.Contains(err.Error(), "hunter2-env") {
		t.Fatalf("environment error must be masked: code=%d err=%v", code, err)
	}
	_, _, code, err = RunSummary(context.Background(), Options{Config: Config{Project: p, Env: "dev"},
		Features: []godog.Feature{{Name: "t.feature", Contents: []byte("Feature: t\n  Scenario: s\n    When I run \"login\" with:\n      | bad name | x |\n")}}})
	if code != ExitUsage || err == nil {
		t.Fatalf("table error must be a usage error: code=%d err=%v", code, err)
	}
	// A non-feature file path is a usage error.
	_, _, code, err = RunSummary(context.Background(), Options{Config: Config{Project: p, Env: "dev"}, Paths: []string{"api.http"}})
	if code != ExitUsage || err == nil || !strings.Contains(err.Error(), "not a .feature file") {
		t.Fatalf("non-feature path: code=%d err=%v", code, err)
	}
}

func TestShortSecretsMaskedInErrors(t *testing.T) {
	srv := server(t)
	p := newProject(t, srv)
	_, _, code, err := RunSummary(context.Background(), Options{Config: Config{Project: p, Env: "dev", Redact: true, Vars: map[string]string{"e": "zz"}},
		Features: []godog.Feature{{Name: "s.feature", Contents: []byte("Feature: s\n  Scenario: s\n    Given the environment is \"{{e}}\"\n")}}})
	if code != ExitUsage || err == nil || strings.Contains(err.Error(), `"zz"`) || !strings.Contains(err.Error(), "***") {
		t.Fatalf("short secret must be masked in error text: code=%d err=%v", code, err)
	}
}

func TestMalformedVariableTableIsUsageError(t *testing.T) {
	srv := server(t)
	p := newProject(t, srv)
	_, _, code, err := RunSummary(context.Background(), Options{Config: Config{Project: p, Env: "dev"},
		Features: []godog.Feature{{Name: "t.feature", Contents: []byte("Feature: t\n  Scenario: s\n    Given the variables:\n      | only |\n")}}})
	if code != ExitUsage || err == nil || !strings.Contains(err.Error(), "two cells") {
		t.Fatalf("code=%d err=%v", code, err)
	}
}

func TestRedactMasksValuesCapturedAfterTheyAppear(t *testing.T) {
	srv := server(t)
	p := newProject(t, srv)
	var out bytes.Buffer
	code, err := Run(context.Background(), Options{
		Config: Config{Project: p, Env: "dev", Redact: true, Stderr: io.Discard},
		Format: "pretty", NoColors: true, Output: &out,
		Features: []godog.Feature{{Name: "late.feature", Contents: []byte(`
Feature: Late capture
  Scenario: The literal appears before the capture registers it
    Given I am logged in
    Then the response body "$.token" is "t-1"
    When I capture the response body "$.token" as "kept"
`)}},
	})
	if err != nil || code != ExitPassed {
		t.Fatalf("code=%d err=%v\n%s", code, err, out.String())
	}
	if strings.Contains(out.String(), "t-1") || !strings.Contains(out.String(), "***") {
		t.Fatalf("a value captured later must be masked in earlier lines:\n%s", out.String())
	}
}

func TestJSONMatchComparesNumbersExactly(t *testing.T) {
	if ok, _ := jsonEqual([]byte(`{"id": 9007199254740993}`), []byte(`{"id": 9007199254740992}`)); ok {
		t.Fatal("large integers must not be rounded to the same value")
	}
	if ok, why := jsonEqual([]byte(`{"n": 1.0, "big": 12345678901234567890}`), []byte(`{"n": 1, "big": 12345678901234567890}`)); !ok {
		t.Fatalf("1.0 and 1 are the same number: %s", why)
	}
	if ok, why := jsonContains([]byte(`{"n": 1e2, "s": "x"}`), []byte(`{"n": 100}`)); !ok {
		t.Fatalf("1e2 and 100 are the same number: %s", why)
	}
	if ok, why := jsonEqual([]byte(`[1]`), []byte(`["1"]`)); ok || !strings.Contains(why, "expected \"1\", got 1") {
		t.Fatalf("a number is not a string: ok=%v why=%s", ok, why)
	}
	if ok, _ := jsonEqual([]byte(`{"n": 1e1000000000}`), []byte(`{"n": 1e1000000000}`)); !ok {
		t.Fatal("an absurd exponent is compared as text, not expanded")
	}
	if ok, _ := jsonEqual([]byte(`{"n": 1e1000000000}`), []byte(`{"n": 2e1000000000}`)); ok {
		t.Fatal("different absurd numbers differ as text")
	}
	for _, bad := range []string{"{}]", "{} {}", "[1],", "{}}"} {
		if ok, _ := jsonEqual([]byte(bad), []byte(`{}`)); ok {
			t.Fatalf("trailing data must be rejected: %q", bad)
		}
	}
	if ok, why := jsonEqual([]byte(" {} \n"), []byte(`{}`)); !ok {
		t.Fatalf("surrounding whitespace is fine: %s", why)
	}
}

func TestRedactMasksXMLEscapedSecretsInJUnit(t *testing.T) {
	srv := server(t)
	p := newProject(t, srv)
	var out bytes.Buffer
	code, err := Run(context.Background(), Options{
		Config: Config{Project: p, Env: "dev", Redact: true, Stderr: io.Discard, Vars: map[string]string{"token": "amp&secret<1>"}},
		Format: "junit", NoColors: true, Output: &out,
		Features: []godog.Feature{{Name: "x.feature", Contents: []byte(`
Feature: Escaped
  Scenario: The name mentions amp&secret<1> and the formatter escapes it
    Given I am logged in
`)}},
	})
	if err != nil || code != ExitPassed {
		t.Fatalf("code=%d err=%v\n%s", code, err, out.String())
	}
	if strings.Contains(out.String(), "amp&amp;secret") || strings.Contains(out.String(), "amp&secret") || !strings.Contains(out.String(), "***") {
		t.Fatalf("the XML-escaped secret leaked:\n%s", out.String())
	}
}

func TestRedactKeepsReportStructureWhenSecretsLookStructural(t *testing.T) {
	srv := server(t)
	p := newProject(t, srv)
	feature := `
Feature: Structural
  Scenario: A failing scenario stays failed
    Given I am logged in
    Then the response status is 500
`
	sum, report, code, err := RunSummary(context.Background(), Options{
		Config:   Config{Project: p, Env: "dev", Redact: true, Stderr: io.Discard, Vars: map[string]string{"a": "failed", "b": "scenario", "c": "Given "}},
		Features: []godog.Feature{{Name: "s.feature", Contents: []byte(feature)}},
	})
	if err != nil || code != ExitFailed || sum.Failed != 1 || sum.OK {
		t.Fatalf("a secret equal to a status must not hide the failure: code=%d err=%v sum=%+v\n%s", code, err, sum, report)
	}
	var out bytes.Buffer
	code, err = Run(context.Background(), Options{
		Config: Config{Project: p, Env: "dev", Redact: true, Stderr: io.Discard, Vars: map[string]string{"a": "testcase", "b": "testsuite", "c": "failure", "d": "failed"}},
		Format: "junit", NoColors: true, Output: &out,
		Features: []godog.Feature{{Name: "s.feature", Contents: []byte(feature)}},
	})
	if err != nil || code != ExitFailed {
		t.Fatalf("code=%d err=%v\n%s", code, err, out.String())
	}
	var suites struct {
		Suites []struct {
			Cases []struct {
				Name    string    `xml:"name,attr"`
				Failure *struct{} `xml:"failure"`
			} `xml:"testcase"`
		} `xml:"testsuite"`
	}
	if err := xml.Unmarshal(out.Bytes(), &suites); err != nil || len(suites.Suites) != 1 || len(suites.Suites[0].Cases) != 1 || suites.Suites[0].Cases[0].Failure == nil {
		t.Fatalf("the JUnit report must stay well-formed with its failure: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), `status="failed"`) || strings.Contains(out.String(), "stays failed") {
		t.Fatalf("structural attributes stay, names are masked:\n%s", out.String())
	}
}

func TestUnnamedRequestFailureIsIdentified(t *testing.T) {
	srv := server(t)
	dir := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dir, "http-client.env.json"), []byte(`{"dev":{"baseUrl":"`+srv.URL+`"}}`), 0o644))
	must(t, os.WriteFile(filepath.Join(dir, "flow.http"), []byte("### unnamed\n# @assert status == 999\nGET {{baseUrl}}/users/0\n"), 0o644))
	p, err := project.Load(dir)
	must(t, err)
	sum, _, code, err := RunSummary(context.Background(), Options{Config: Config{Project: p, Env: "dev"},
		Features: []godog.Feature{{Name: "f.feature", Contents: []byte("Feature: f\n  Scenario: s\n    When I run the file \"flow.http\"\n")}}})
	if err != nil || code != ExitFailed || len(sum.Failures) != 1 || !strings.HasPrefix(sum.Failures[0].Error, "flow.http:3 failed") {
		t.Fatalf("code=%d err=%v sum=%+v", code, err, sum)
	}
}

func TestMalformedTagExpressionIsUsageError(t *testing.T) {
	srv := server(t)
	p := newProject(t, srv)
	for _, bad := range []string{"@smoke && ", "@", ",", "~", "@a && ~", "@a b", "@smoke||@slow", "@smoke&@slow", "!@slow", "(@a)"} {
		_, _, code, err := RunSummary(context.Background(), Options{Config: Config{Project: p, Env: "dev"}, Tags: bad,
			Features: []godog.Feature{{Name: "t.feature", Contents: []byte("Feature: t\n  @smoke\n  Scenario: s\n    Given I am logged in\n")}}})
		if code != ExitUsage || err == nil || !strings.Contains(err.Error(), "invalid tag expression") {
			t.Fatalf("%q: code=%d err=%v", bad, code, err)
		}
	}
	for _, good := range []string{"@smoke", "smoke", "~@slow", "@smoke && ~@slow", "@smoke,@other", " @smoke , ~slow "} {
		if _, _, code, err := RunSummary(context.Background(), Options{Config: Config{Project: p, Env: "dev"}, Tags: good,
			Features: []godog.Feature{{Name: "t.feature", Contents: []byte("Feature: t\n  @smoke\n  Scenario: s\n    Given I am logged in\n")}}}); err != nil || code != ExitPassed {
			t.Fatalf("%q: code=%d err=%v", good, code, err)
		}
	}
}

func TestEnvironmentSwitchKeepsScenarioState(t *testing.T) {
	srv := server(t)
	p := newProject(t, srv)
	sum, code := run(t, p, `
Feature: Switch
  Scenario: Variables, captures and the last response survive an environment switch
    Given I am logged in
    And the variable "who" is "alice"
    When the environment is "dev"
    Then the variable "check" is "{{token}}-{{who}}"
    And the variable "check" is "t-1-alice"
    And the response status is 200
`, "dev")
	if code != ExitPassed || !sum.OK {
		t.Fatalf("code=%d sum=%+v", code, sum)
	}
}

func TestEnvironmentSwitchCarriesSessionValues(t *testing.T) {
	srv := server(t)
	dir := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dir, "api.http"), []byte(apiHTTP), 0o644))
	must(t, os.WriteFile(filepath.Join(dir, "http-client.env.json"), []byte(`{"dev":{"baseUrl":"`+srv.URL+`","role":"member"},"alt":{"baseUrl":"`+srv.URL+`","role":"member"}}`), 0o644))
	p, err := project.Load(dir)
	must(t, err)
	sum, _, code, err := RunSummary(context.Background(), Options{
		Config: Config{Project: p, Env: "dev", UseSession: true, Stderr: io.Discard},
		Features: []godog.Feature{{Name: "s.feature", Contents: []byte(`
Feature: Session across environments
  Scenario: Log in under dev
    Given I am logged in
  Scenario: A later scenario switches environments and still sees the token
    When the environment is "alt"
    Then the variable "check" is "{{token}}"
    And the variable "check" is "t-1"
`)}},
	})
	if err != nil || code != ExitPassed || !sum.OK {
		t.Fatalf("code=%d err=%v sum=%+v", code, err, sum)
	}
}

func TestEnvironmentSwitchCarriesAuthCache(t *testing.T) {
	srv := server(t)
	dir := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dir, "api.http"), []byte(apiHTTP), 0o644))
	must(t, os.WriteFile(filepath.Join(dir, "http-client.env.json"), []byte(`{"dev":{"baseUrl":"`+srv.URL+`","role":"member"},"alt":{"baseUrl":"`+srv.URL+`","role":"member"}}`), 0o644))
	must(t, os.MkdirAll(filepath.Join(dir, ".apic"), 0o755))
	must(t, os.WriteFile(filepath.Join(dir, ".apic", "session.json"), []byte(`{"envs":{"dev":{"$oauth2:abc":"cached-token"}}}`), 0o644))
	p, err := project.Load(dir)
	must(t, err)
	_, _, code, err := RunSummary(context.Background(), Options{
		Config: Config{Project: p, Env: "dev", UseSession: true, Stderr: io.Discard},
		Features: []godog.Feature{{Name: "s.feature", Contents: []byte(`
Feature: Auth cache across environments
  Scenario: The cache follows the scenario into the new environment and is saved
    When the environment is "alt"
`)}},
	})
	if err != nil || code != ExitPassed {
		t.Fatalf("code=%d err=%v", code, err)
	}
	data, err := os.ReadFile(filepath.Join(dir, ".apic", "session.json"))
	must(t, err)
	var saved struct {
		Envs map[string]map[string]string `json:"envs"`
	}
	must(t, json.Unmarshal(data, &saved))
	if saved.Envs["alt"]["$oauth2:abc"] != "cached-token" {
		t.Fatalf("the auth cache must be persisted under the new environment even without a later capture: %s", data)
	}
}

func TestMalformedFeatureIsUsageErrorWithPosition(t *testing.T) {
	srv := server(t)
	p := newProject(t, srv)
	sum, _, code, err := RunSummary(context.Background(), Options{Config: Config{Project: p, Env: "dev", Stderr: io.Discard},
		Features: []godog.Feature{{Name: "bad.feature", Contents: []byte("Feature: x\n  Scenario: s\n    Given I am logged in\nFeature: y\n")}}})
	if code != ExitUsage || err == nil || !strings.Contains(err.Error(), "bad.feature") || !strings.Contains(err.Error(), "(4:1)") || sum != nil {
		t.Fatalf("code=%d err=%v sum=%+v", code, err, sum)
	}
	must(t, os.MkdirAll(filepath.Join(p.Root, "features"), 0o755))
	must(t, os.WriteFile(filepath.Join(p.Root, "features", "broken.feature"), []byte("Feature: x\n  Scenario: s\n    Given I am logged in\n  Scenario Outline: o\n    Given I am logged in\n    Examples:\n      | a |\n      | 1 | 2 |\n"), 0o644))
	_, _, code, err = RunSummary(context.Background(), Options{Config: Config{Project: p, Env: "dev", Stderr: io.Discard}})
	if code != ExitUsage || err == nil || !strings.Contains(err.Error(), "broken.feature") {
		t.Fatalf("code=%d err=%v", code, err)
	}
}

func TestSelectorsAreRenderedAndResponseRefsSurviveEnvSwitch(t *testing.T) {
	srv := server(t)
	p := newProject(t, srv)
	sum, code := run(t, p, `
Feature: Rendered selectors
  Scenario: Selectors may contain variables and named responses survive a switch
    Given the variable "idPath" is "$.token"
    And the variable "hdr" is "content-type"
    And I am logged in
    Then the response body "{{idPath}}" is "t-1"
    And the response body "{{idPath}}" exists
    And the response header "{{hdr}}" contains "json"
    When I capture the response body "{{idPath}}" as "tok"
    Then the variable "check" is "{{tok}}"
    And the variable "check" is "t-1"
    When the environment is "dev"
    Then the variable "ref" is "{{login.response.body.$.token}}"
    And the variable "ref" is "t-1"
`, "dev")
	if code != ExitPassed || !sum.OK {
		t.Fatalf("code=%d sum=%+v", code, sum)
	}
}

func TestConfiguredTestPathsAndExplicitOverride(t *testing.T) {
	srv := server(t)
	dir := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dir, "api.http"), []byte(apiHTTP), 0o644))
	must(t, os.WriteFile(filepath.Join(dir, "http-client.env.json"), []byte(`{"dev":{"baseUrl":"`+srv.URL+`","role":"member"}}`), 0o644))
	must(t, os.WriteFile(filepath.Join(dir, "apic.yaml"), []byte("test:\n  paths: [specs]\n"), 0o644))
	must(t, os.MkdirAll(filepath.Join(dir, "specs"), 0o755))
	must(t, os.MkdirAll(filepath.Join(dir, "features"), 0o755))
	must(t, os.WriteFile(filepath.Join(dir, "specs", "ok.feature"), []byte("Feature: ok\n  Scenario: passes\n    Given I am logged in\n"), 0o644))
	must(t, os.WriteFile(filepath.Join(dir, "features", "bad.feature"), []byte("Feature: bad\n  Scenario: fails\n    Given I am logged in\n    Then the response status is 500\n"), 0o644))
	p, err := project.Load(dir)
	must(t, err)
	if len(p.Config.Test.Paths) != 1 || p.Config.Test.Paths[0] != "specs" {
		t.Fatalf("test.paths not loaded: %+v", p.Config.Test)
	}
	sum, _, code, err := RunSummary(context.Background(), Options{Config: Config{Project: p, Env: "dev", Stderr: io.Discard}})
	if err != nil || code != ExitPassed || sum.Scenarios != 1 || sum.Failed != 0 {
		t.Fatalf("configured paths: code=%d err=%v sum=%+v", code, err, sum)
	}
	sum, _, code, err = RunSummary(context.Background(), Options{Config: Config{Project: p, Env: "dev", Stderr: io.Discard}, Paths: []string{"features"}})
	if err != nil || code != ExitFailed || sum.Scenarios != 1 || sum.Failed != 1 {
		t.Fatalf("explicit paths must override test.paths: code=%d err=%v sum=%+v", code, err, sum)
	}
}

func TestPhraseRunsRequestFromFileNameWithHash(t *testing.T) {
	srv := server(t)
	dir := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dir, "odd#name.http"), []byte("### a\n# @name login\n# @step I log in oddly\n# @capture token = body.$.token\nPOST {{baseUrl}}/login\n"), 0o644))
	must(t, os.WriteFile(filepath.Join(dir, "http-client.env.json"), []byte(`{"dev":{"baseUrl":"`+srv.URL+`"}}`), 0o644))
	p, err := project.Load(dir)
	must(t, err)
	sum, code := run(t, p, "Feature: h\n  Scenario: s\n    Given I log in oddly\n    Then the response status is 200\n    And the variable \"check\" is \"{{token}}\"\n    When I run the file \"odd#name.http\"\n    Then the response status is 200\n", "dev")
	if code != ExitPassed || !sum.OK {
		t.Fatalf("code=%d sum=%+v", code, sum)
	}
}

func TestVariableNamesCannotLookLikeResponseReferences(t *testing.T) {
	if err := checkVarName("foo.response.bar"); err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Fatalf("expected a usage error: %v", err)
	}
	if err := checkVarName("foo.bar"); err != nil {
		t.Fatalf("dots are otherwise fine: %v", err)
	}
	if err := checkVarName("a;b"); err == nil || strings.Contains(err.Error(), "- ;") {
		t.Fatalf("the message must not advertise characters the grammar rejects: %v", err)
	}
}

func TestStopOnFailureCountsSkippedScenarios(t *testing.T) {
	srv := server(t)
	p := newProject(t, srv)
	sum, _, code, err := RunSummary(context.Background(), Options{
		Config: Config{Project: p, Env: "dev", Stderr: io.Discard}, StopOnFailure: true,
		Features: []godog.Feature{{Name: "s.feature", Contents: []byte(`
Feature: Stop
  Scenario: Fails first
    Given I am logged in
    Then the response status is 500
  Scenario: Never runs
    Given I am logged in
    Then the response status is 200
`)}},
	})
	if err != nil || code != ExitFailed || sum.Scenarios != 2 || sum.Failed != 1 || sum.Passed != 0 || sum.Skipped != 1 || sum.OK {
		t.Fatalf("the scenario that never ran must be reported as skipped: code=%d err=%v sum=%+v", code, err, sum)
	}
}

func TestRedactMasksCaptureStepErrors(t *testing.T) {
	srv := server(t)
	p := newProject(t, srv)
	sum, _, code, err := RunSummary(context.Background(), Options{
		Config: Config{Project: p, Env: "dev", Redact: true, Stderr: io.Discard, Vars: map[string]string{"short": "zq", "long": "hidden-path-secret"}},
		Features: []godog.Feature{{Name: "c.feature", Contents: []byte(`
Feature: Capture errors
  Scenario: Missing capture path with a short secret
    Given I am logged in
    When I capture the response body "$.{{short}}" as "a"
  Scenario: Missing capture path with a long secret
    Given I am logged in
    When I capture the response body "$.{{long}}" as "b"
`)}},
	})
	if err != nil || code != ExitFailed || sum.Failed != 2 {
		t.Fatalf("code=%d err=%v sum=%+v", code, err, sum)
	}
	for _, f := range sum.Failures {
		if strings.Contains(f.Error, "zq") || strings.Contains(f.Error, "hidden-path-secret") {
			t.Fatalf("capture errors must be masked: %q", f.Error)
		}
	}
}

func TestRedactMasksShortSecretInPath(t *testing.T) {
	srv := server(t)
	p := newProject(t, srv)
	sum, _, code, err := RunSummary(context.Background(), Options{
		Config: Config{Project: p, Env: "dev", Redact: true, Stderr: io.Discard, Vars: map[string]string{"userId": "zq"}},
		Features: []godog.Feature{{Name: "p.feature", Contents: []byte(`
Feature: Path secrets
  Scenario: A two-character secret in the path never appears in a failure
    Given I am logged in
    When I run "get-user"
    Then the response status is 200
`)}},
	})
	if err != nil || code != ExitFailed || len(sum.Failures) != 1 {
		t.Fatalf("code=%d err=%v sum=%+v", code, err, sum)
	}
	if e := sum.Failures[0].Error; strings.Contains(e, "/users/zq") || strings.Contains(e, "zq") {
		t.Fatalf("the path secret leaked: %q", e)
	}
}

func TestRunFileStepRefusesRequestIDs(t *testing.T) {
	srv := server(t)
	p := newProject(t, srv)
	_, _, code, err := RunSummary(context.Background(), Options{Config: Config{Project: p, Env: "dev", Stderr: io.Discard},
		Features: []godog.Feature{{Name: "f.feature", Contents: []byte("Feature: f\n  Scenario: s\n    When I run the file \"login\"\n")}}})
	if code != ExitUsage || err == nil || !strings.Contains(err.Error(), "not a .http/.rest file") {
		t.Fatalf("code=%d err=%v", code, err)
	}
	_, _, code, err = RunSummary(context.Background(), Options{Config: Config{Project: p, Env: "dev", Stderr: io.Discard},
		Features: []godog.Feature{{Name: "f.feature", Contents: []byte("Feature: f\n  Scenario: s\n    When I run the file \"api.http#login\"\n")}}})
	if code != ExitUsage || err == nil {
		t.Fatalf("a fragment is not a whole file: code=%d err=%v", code, err)
	}
}

func TestTableRowsRenderInOrder(t *testing.T) {
	srv := server(t)
	p := newProject(t, srv)
	// Enough chained rows that map iteration would almost surely break it.
	sum, code := run(t, p, `
Feature: Ordered tables
  Scenario: Later rows see the rows above them
    Given I am logged in
    And the variables:
      | name  | value      |
      | a     | get        |
      | b     | {{a}}-user |
      | c     | {{b}}      |
      | which | {{c}}      |
    Then the variable "which" is "get-user"
    When I run "{{target}}" with:
      | id     | 0         |
      | userId | {{id}}    |
      | target | {{which}} |
    Then the response status is 404
    And the response status is not 200
`, "dev")
	if code != ExitPassed || !sum.OK {
		t.Fatalf("code=%d sum=%+v", code, sum)
	}
	// A row that refers to one below it is a missing variable, not a race.
	var stderr bytes.Buffer
	_, _, code, err := RunSummary(context.Background(), Options{
		Config: Config{Project: p, Env: "dev", Stderr: &stderr},
		Features: []godog.Feature{{Name: "t.feature", Contents: []byte(`
Feature: t
  Scenario: s
    Given the variables:
      | x | {{y}} |
      | y | 1     |
`)}},
	})
	if code != ExitUsage || err == nil || !strings.Contains(err.Error(), "missing variables: y") {
		t.Fatalf("forward reference: code=%d err=%v stderr=%s", code, err, stderr.String())
	}
}

func TestEmptyFileDoesNotKeepThePreviousResponse(t *testing.T) {
	srv := server(t)
	p := newProject(t, srv)
	must(t, os.WriteFile(filepath.Join(p.Root, "empty.http"), []byte("# only a comment\n"), 0o644))
	p, err := project.Load(p.Root)
	must(t, err)
	var stderr bytes.Buffer
	sum, _, code, err := RunSummary(context.Background(), Options{
		Config: Config{Project: p, Env: "dev", Stderr: &stderr},
		Features: []godog.Feature{{Name: "e.feature", Contents: []byte(`
Feature: Empty
  Scenario: A file with no requests cannot stand in for a response
    When I run "login"
    And I run the file "empty.http"
    Then the response status is 200
`)}},
	})
	if code != ExitUsage || err == nil || !strings.Contains(err.Error(), "no requests") || sum.OK {
		t.Fatalf("the empty file must be a usage error, not a pass on the login response: code=%d err=%v sum=%+v", code, err, sum)
	}
}

// TestStepRunsRefsFirst: a request's `# @ref` applies in a scenario too, so
// a step can be the first line of a scenario without a login step before it.
func TestStepRunsRefsFirst(t *testing.T) {
	srv := server(t)
	p := newProject(t, srv)
	sum, code := run(t, p, `
Feature: Refs
  Scenario: fetch without logging in first
    Given a user named "bob" exists
    When I fetch user {{userId}} logged in
    Then the response status is 200
    And the response body "$.name" is "bob"
`, "dev")
	if code != 0 || sum.Failed != 0 {
		t.Fatalf("code=%d summary=%+v", code, sum)
	}
}

func TestCookieStepsAndPerScenarioJar(t *testing.T) {
	srv := server(t)
	p := newProject(t, srv)
	feature := `
Feature: Cookies
  Scenario: A session cookie carries over within a scenario
    When I log in with a form
    Then the response status is 204
    And the response cookie "sid" exists
    And the response cookie "sid" is "c-1"
    And the response cookie "sid" starts with "c-"
    And the response cookie "nope" does not exist
    When I capture the response cookie "sid" as "sid"
    And I ask who I am
    Then the response status is 200

  Scenario: The next scenario starts with an empty jar
    When I ask who I am
    Then the response status is 401
`
	var stderr bytes.Buffer
	sum, report, code, err := RunSummary(context.Background(), Options{
		Config:   Config{Project: p, Env: "dev", Cookies: true, Stderr: &stderr},
		Features: []godog.Feature{{Name: "cookies.feature", Contents: []byte(feature)}},
	})
	if err != nil {
		t.Fatalf("run: %v\nreport: %s", err, report)
	}
	if code != 0 || sum.Failed != 0 || sum.Passed != 2 {
		t.Fatalf("code=%d summary=%+v\n%s", code, sum, report)
	}
	// Without the jar the cookie is still visible to the steps, just not sent.
	sum, code = run(t, p, `
Feature: No jar
  Scenario: Cookies are inspected but not kept
    When I log in with a form
    Then the response cookie "sid" is "c-1"
    When I ask who I am
    Then the response status is 401
`, "dev")
	if code != 0 || sum.Failed != 0 {
		t.Fatalf("code=%d summary=%+v", code, sum)
	}
}

// The JSONPath forms work the same in steps as in # @assert: filters,
// recursive descent, negative indexes, slices, .length and multi-valued
// headers, with or without the leading $.
func TestSelectorForms(t *testing.T) {
	srv := server(t)
	p := newProject(t, srv)
	sum, code := run(t, p, `
Feature: Selectors
  Scenario: JSONPath
    When I list the catalog
    Then the response body "$.items[-1].id" is "c"
    And the response body "items[?(@.done == true)].id" contains "c"
    And the response body "$.items[?(@.name =~ /^a/)].length" is "2"
    And the response body "$..id" contains "u1"
    And the response body "..price" contains "0.25"
    And the response body "$.items[1:3].length" is "2"
    And the response body "items.length" is "3"
    And the response body "$.items[?(@.price > 5)]" does not exist
    And the response header "link.#" is "2"
    And the response header "link[-1]" contains "rel=last"
`, "dev")
	if code != 0 || sum.Failed != 0 {
		t.Fatalf("code=%d summary=%+v", code, sum)
	}
}
