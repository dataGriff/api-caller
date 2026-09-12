package openapi

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dataGriff/api-caller/internal/project"
)

func TestImportPetstore(t *testing.T) {
	dir := t.TempDir()
	res, err := Import("testdata/petstore.yaml", Options{OutDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if res.Requests != 4 || len(res.Files) != 2 || res.BaseURL != "https://petstore.example.com/v1" {
		t.Fatalf("%+v", res)
	}
	pets, err := os.ReadFile(filepath.Join(dir, "pets.http"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(pets)
	for _, want := range []string{
		"# @name list-pets", "GET {{baseUrl}}/pets\n", "    # ?limit={{limit}}  (optional)", "X-Trace: {{xTrace}}",
		"# @name create-pet", "# @assert status == 201", "# @description Creates a pet.", "Content-Type: application/json", `"name": "Rex"`, `"born": "{{$isoTimestamp}}"`,
		"# @name show-pet-by-id", "GET {{baseUrl}}/pets/{{petId}}",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("pets.http missing %q\n%s", want, s)
		}
	}
	api, _ := os.ReadFile(filepath.Join(dir, "api.http"))
	if !strings.Contains(string(api), "# @name get-health") {
		t.Errorf("api.http:\n%s", api)
	}
	env, _ := os.ReadFile(filepath.Join(dir, "http-client.env.json"))
	if !strings.Contains(string(env), `"baseUrl": "https://petstore.example.com/v1"`) {
		t.Errorf("env: %s", env)
	}

	// The generated files must parse cleanly and be runnable.
	p, err := project.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range p.Validate() {
		if d.Severity == "error" {
			t.Errorf("validate: %s", d)
		}
	}
	if len(p.Requests()) != 4 {
		t.Fatalf("parsed %d requests", len(p.Requests()))
	}

	// Existing files are not overwritten without --force.
	res2, err := Import("testdata/petstore.yaml", Options{OutDir: dir})
	if err != nil || len(res2.Skipped) != 2 || len(res2.Files) != 0 {
		t.Fatalf("%+v %v", res2, err)
	}
}
