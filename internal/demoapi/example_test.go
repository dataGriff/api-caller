package demoapi

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/dataGriff/api-caller/internal/bdd"
	"github.com/dataGriff/api-caller/internal/project"
	"github.com/dataGriff/api-caller/internal/runner"
)

// TestExamplesSuite runs every request in project/ as one flow against a
// live instance of the demo API, so the bundled example stays correct
// without needing network access.
func TestExamplesSuite(t *testing.T) {
	srv := httptest.NewServer(New())
	defer srv.Close()

	p, err := project.Load("project")
	if err != nil {
		t.Fatal(err)
	}
	r, err := runner.New(p, runner.Options{
		Vars:      map[string]string{"baseUrl": srv.URL},
		NoSession: true,
		KeepGoing: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	results, err := r.RunAll(context.Background(), p.Requests())
	if err != nil {
		t.Fatal(err)
	}

	wantFail := map[string]bool{"not-found": true}
	for _, res := range results {
		name := res.Request.Name
		if res.OK == wantFail[name] {
			t.Errorf("%s: OK=%v, errors=%v, asserts=%+v", name, res.OK, res.Errors, res.Asserts)
		}
	}
}

// TestExampleFeatures runs the bundled features/ against the demo API, so
// `apic test -C apic-demo` keeps working offline.
func TestExampleFeatures(t *testing.T) {
	srv := httptest.NewServer(New())
	defer srv.Close()

	p, err := project.Load("project")
	if err != nil {
		t.Fatal(err)
	}
	sum, _, code, err := bdd.RunSummary(context.Background(), bdd.Options{
		Config: bdd.Config{Project: p, Vars: map[string]string{"baseUrl": srv.URL}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 || !sum.OK || sum.Scenarios != 8 || sum.Passed != 8 {
		t.Fatalf("code=%d summary=%+v", code, sum)
	}
}
