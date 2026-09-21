package runner

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestTimingsBreakdownAndConnectionReuse(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(15 * time.Millisecond)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	dir := writeProject(t, map[string]string{
		"http-client.env.json": `{"dev": {"baseUrl": "` + srv.URL + `"}}`,
		"api.http":             "### a\n# @name a\nGET {{baseUrl}}/a\n\n### b\n# @name b\nGET {{baseUrl}}/b\n",
	})
	r := newRunner(t, dir, Options{Env: "dev", NoSession: true, Insecure: true})
	req, _ := r.Project.Lookup("a")
	res, err := r.Run(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	tm := res.Response.Timings
	if tm == nil {
		t.Fatal("no timings")
	}
	// A first request resolves, connects and shakes hands; each stage is
	// bounded by the next and the whole by the round trip.
	if tm.Reused || tm.TLSMs < 0 || tm.ConnectMs < 0 || tm.DNSMs < 0 {
		t.Fatalf("first request: %+v", tm)
	}
	if tm.TTFBMs < 15 || tm.TTFBMs > tm.TotalMs || tm.TotalMs != res.Response.DurationMs {
		t.Fatalf("ttfb/total: %+v duration=%d", tm, res.Response.DurationMs)
	}
	data, _ := json.Marshal(res)
	if !strings.Contains(string(data), `"timings":{"dns_ms":`) || !strings.Contains(string(data), `"reused":false}`) {
		t.Fatalf("json: %s", data)
	}
	// The second request in the same invocation rides the same connection.
	req, _ = r.Project.Lookup("b")
	res, err = r.Run(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	tm = res.Response.Timings
	if !tm.Reused || tm.DNSMs != 0 || tm.ConnectMs != 0 || tm.TLSMs != 0 || tm.TTFBMs < 15 {
		t.Fatalf("second request: %+v", tm)
	}
}
