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

  Scenario: Switch environment
    Given the environment is "other"
    When I run "login"
`, "dev")
	if code != ExitFailed || sum.Passed != 2 || sum.Failed != 1 {
		t.Fatalf("code=%d summary=%+v", code, sum)
	}
	if _, err := os.Stat(filepath.Join(p.Root, ".apic")); !os.IsNotExist(err) {
		t.Fatal("tests must not write the session")
	}
	if !strings.Contains(sum.Failures[0].Error, "request failed") {
		t.Fatalf("environment switch should hit the unreachable env: %+v", sum.Failures)
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
