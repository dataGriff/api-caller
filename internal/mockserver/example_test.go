package mockserver

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/dataGriff/api-caller/internal/project"
	"github.com/dataGriff/api-caller/internal/runner"
)

// TestExamplesMockSuite runs every request in examples/mock as one flow
// against a live instance of the mock API, so the bundled example stays
// correct without needing network access.
func TestExamplesMockSuite(t *testing.T) {
	srv := httptest.NewServer(New())
	defer srv.Close()

	p, err := project.Load("../../examples/mock")
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
