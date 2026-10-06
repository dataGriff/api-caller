package runner

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dataGriff/api-caller/internal/session"
)

func TestHistoryRecordsEachRequestARefRan(t *testing.T) {
	dir, _ := refProject(t)
	if err := os.WriteFile(filepath.Join(dir, "apic.yaml"), []byte("history: 3\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := newRunner(t, dir, Options{Env: "dev", Session: session.NewMemory()})
	at := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	r.Now = func() time.Time { return at }
	if r.History == nil {
		t.Fatal("history: 3 should switch history on")
	}
	res, err := r.Run(context.Background(), lookup(t, r, "whoami"))
	if err != nil || !res.OK {
		t.Fatalf("whoami: %+v %v", res, err)
	}
	for _, name := range []string{"login", "whoami"} {
		entries, err := r.History.List("dev", name)
		if err != nil || len(entries) != 1 {
			t.Fatalf("%s: %v %v", name, entries, err)
		}
		if !entries[0].Time.Equal(at) || entries[0].Status != 200 {
			t.Errorf("%s entry = %+v", name, entries[0])
		}
	}
	e, err := r.History.Get("dev", "whoami", 1)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := ParseResult(e.Result)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.Deps) != 0 || stored.Request.Name != "whoami" || stored.Raw() == nil || stored.Raw().Status != 200 {
		t.Errorf("stored whoami = %+v", stored)
	}
	if got := string(stored.Raw().Body); got != `{"email":"alice@example.com","token":"tok-1"}` {
		t.Errorf("stored body = %s", got)
	}
	for _, h := range stored.Request.Headers {
		if h.Name == "Authorization" && h.Value != Masked {
			t.Errorf("Authorization stored as %q", h.Value)
		}
	}
}

func TestNoHistoryAndNoSessionRecordNothing(t *testing.T) {
	dir, _ := refProject(t)
	if err := os.WriteFile(filepath.Join(dir, "apic.yaml"), []byte("history: 3\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, opts := range []Options{{Env: "dev", NoSession: true}, {Env: "dev", NoHistory: true}} {
		if r := newRunner(t, dir, opts); r.History != nil {
			t.Errorf("%+v: history is on", opts)
		}
	}
}
