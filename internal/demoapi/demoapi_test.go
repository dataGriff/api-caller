package demoapi

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestStatusOutOfRangeFallsBackTo200(t *testing.T) {
	srv := httptest.NewServer(New())
	defer srv.Close()

	res, err := http.Get(srv.URL + "/status/9999")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode != http.StatusOK {
		t.Fatalf("status code = %d, want %d", res.StatusCode, http.StatusOK)
	}
}

// reply is what call returns: the status, headers and the JSON body decoded
// as an object or an array, whichever it was.
type reply struct {
	StatusCode int
	Header     http.Header
	obj        map[string]any
	arr        []any
}

// call sends one request with a bearer token and decodes the JSON body.
func call(t *testing.T, srv *httptest.Server, method, path string, body string, hdr map[string]string) (reply, map[string]any, []any) {
	t.Helper()
	req, err := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer mock-token")
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	data, _ := io.ReadAll(res.Body)
	var obj map[string]any
	var arr []any
	if json.Unmarshal(data, &obj) != nil {
		_ = json.Unmarshal(data, &arr)
	}
	return reply{StatusCode: res.StatusCode, Header: res.Header, obj: obj, arr: arr}, obj, arr
}

func TestTodosFilterAndPaginate(t *testing.T) {
	srv := httptest.NewServer(New())
	defer srv.Close()

	res, _, all := call(t, srv, "GET", "/todos", "", nil)
	if res.StatusCode != 200 || len(all) != 2 || res.Header.Get("X-Total-Count") != "2" || res.Header.Get("Link") != "" {
		t.Fatalf("unfiltered: status=%d len=%d total=%q link=%q", res.StatusCode, len(all), res.Header.Get("X-Total-Count"), res.Header.Get("Link"))
	}
	res, _, done := call(t, srv, "GET", "/todos?done=true", "", nil)
	if len(done) != 1 || res.Header.Get("X-Total-Count") != "1" {
		t.Fatalf("done=true: %v total=%q", done, res.Header.Get("X-Total-Count"))
	}
	res, _, page := call(t, srv, "GET", "/todos?limit=1", "", nil)
	if len(page) != 1 || res.Header.Get("X-Total-Count") != "2" || !strings.Contains(res.Header.Get("Link"), `page=2`) || !strings.Contains(res.Header.Get("Link"), `rel="next"`) {
		t.Fatalf("limit=1: len=%d total=%q link=%q", len(page), res.Header.Get("X-Total-Count"), res.Header.Get("Link"))
	}
	res, _, last := call(t, srv, "GET", "/todos?limit=1&page=2", "", nil)
	if len(last) != 1 || res.Header.Get("Link") != "" {
		t.Fatalf("last page should carry no next link: len=%d link=%q", len(last), res.Header.Get("Link"))
	}
	res, obj, _ := call(t, srv, "GET", "/todos?done=maybe", "", nil)
	if res.StatusCode != 400 || obj["error"] == nil {
		t.Fatalf("bad done value: %d %v", res.StatusCode, obj)
	}
}

func TestCreateTodoValidates(t *testing.T) {
	srv := httptest.NewServer(New())
	defer srv.Close()

	res, obj, _ := call(t, srv, "POST", "/todos", `{"title": "  "}`, nil)
	if res.StatusCode != 422 {
		t.Fatalf("status = %d, want 422: %v", res.StatusCode, obj)
	}
	fields, _ := obj["fields"].(map[string]any)
	if obj["error"] != "validation failed" || fields["title"] != "must not be empty" {
		t.Fatalf("body = %v", obj)
	}
	res, obj, _ = call(t, srv, "POST", "/todos", `{"title": "ok"}`, nil)
	if res.StatusCode != 201 || obj["id"] != "3" {
		t.Fatalf("valid create: %d %v", res.StatusCode, obj)
	}
}

func TestJobsAdvancePerPoll(t *testing.T) {
	srv := httptest.NewServer(New())
	defer srv.Close()

	res, obj, _ := call(t, srv, "POST", "/jobs", "", nil)
	if res.StatusCode != 202 || obj["state"] != "queued" || res.Header.Get("Location") != "/jobs/1" {
		t.Fatalf("create: %d %v location=%q", res.StatusCode, obj, res.Header.Get("Location"))
	}
	for i, want := range []string{"running", "done", "done"} {
		_, obj, _ = call(t, srv, "GET", "/jobs/1", "", nil)
		if obj["state"] != want {
			t.Fatalf("poll %d: state=%v want %s", i+1, obj["state"], want)
		}
	}
	res, _, _ = call(t, srv, "GET", "/jobs/99", "", nil)
	if res.StatusCode != 404 {
		t.Fatalf("unknown job: %d", res.StatusCode)
	}
}

func TestUploadEchoesParts(t *testing.T) {
	srv := httptest.NewServer(New())
	defer srv.Close()

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("title", "Quarterly report")
	fw, _ := mw.CreateFormFile("file", "report.txt")
	_, _ = fw.Write([]byte("hello"))
	_ = mw.Close()

	req, _ := http.NewRequest("POST", srv.URL+"/upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	var out struct {
		Fields map[string]string `json:"fields"`
		Files  []struct {
			Field, Filename string
			Size            int64
		} `json:"files"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 201 || out.Fields["title"] != "Quarterly report" || len(out.Files) != 1 || out.Files[0].Filename != "report.txt" || out.Files[0].Size != 5 {
		t.Fatalf("status=%d out=%+v", res.StatusCode, out)
	}

	res2, obj, _ := call(t, srv, "POST", "/upload", `{"not": "multipart"}`, nil)
	if res2.StatusCode != 400 || obj["error"] == nil {
		t.Fatalf("non-multipart: %d %v", res2.StatusCode, obj)
	}
}

func TestGraphQL(t *testing.T) {
	srv := httptest.NewServer(New())
	defer srv.Close()

	res, obj, _ := call(t, srv, "POST", "/graphql", `{"query": "{ todos(done: $done) { id } }", "variables": {"done": true}}`, nil)
	data, _ := obj["data"].(map[string]any)
	todos, _ := data["todos"].([]any)
	if res.StatusCode != 200 || len(todos) != 1 {
		t.Fatalf("todos query: %d %v", res.StatusCode, obj)
	}
	_, obj, _ = call(t, srv, "POST", "/graphql", `{"query": "{ nope }"}`, nil)
	if obj["errors"] == nil {
		t.Fatalf("unknown field should report errors: %v", obj)
	}
	res, obj, _ = call(t, srv, "POST", "/graphql", `not json`, nil)
	if res.StatusCode != 400 || obj["errors"] == nil {
		t.Fatalf("bad body: %d %v", res.StatusCode, obj)
	}
}

func TestAPIKeyCSVSlowAndHealth(t *testing.T) {
	srv := httptest.NewServer(New())
	defer srv.Close()

	res, _, _ := call(t, srv, "GET", "/keyed", "", nil)
	if res.StatusCode != 401 {
		t.Fatalf("no key: %d", res.StatusCode)
	}
	res, obj, _ := call(t, srv, "GET", "/keyed", "", map[string]string{"X-Api-Key": "demo-key"})
	if res.StatusCode != 200 || obj["ok"] != true {
		t.Fatalf("with key: %d %v", res.StatusCode, obj)
	}

	req, _ := http.NewRequest("GET", srv.URL+"/reports/daily.csv", nil)
	csvRes, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(csvRes.Body)
	_ = csvRes.Body.Close()
	if !strings.HasPrefix(csvRes.Header.Get("Content-Type"), "text/csv") || !strings.HasPrefix(string(body), "date,total,done,open\n") {
		t.Fatalf("csv: type=%q body=%q", csvRes.Header.Get("Content-Type"), body)
	}

	start := time.Now()
	res, obj, _ = call(t, srv, "GET", "/slow?ms=50", "", nil)
	if res.StatusCode != 200 || obj["slept_ms"] != float64(50) || time.Since(start) < 50*time.Millisecond {
		t.Fatalf("slow: %d %v after %s", res.StatusCode, obj, time.Since(start))
	}

	res, obj, _ = call(t, srv, "GET", "/health", "", nil)
	if res.StatusCode != 200 || obj["status"] != "ok" || obj["uptime_seconds"] == nil {
		t.Fatalf("health: %d %v", res.StatusCode, obj)
	}
}
