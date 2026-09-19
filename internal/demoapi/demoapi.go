// Package demoapi backs `apic demo`: a tiny, fully in-memory HTTP API and
// its matching example .http project (embedded from project/), so apic can
// be tried with no network access and no git clone.
package demoapi

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	demoUser         = "alice"
	demoPassword     = "s3cret"
	demoClientID     = "demo-client"
	demoClientSecret = "demo-secret"
	demoAPIKey       = "demo-key"

	// maxSlowMillis caps GET /slow so a typo cannot park a request for an hour.
	maxSlowMillis = 10_000
	// defaultPageSize is the page size of GET /todos when limit is not given.
	defaultPageSize = 20
	// maxUploadBytes bounds what POST /upload buffers in memory.
	maxUploadBytes = 8 << 20
)

//go:embed project
var projectFS embed.FS

// WriteProject writes the bundled example project into dir (creating it if
// needed): apic.yaml, http-client.private.env.json, the .http files and
// features/ verbatim from project/, plus a generated http-client.env.json
// pointing at http://localhost:<port>. Files that already exist are left
// alone unless force is set.
func WriteProject(dir string, port int, force bool) (written, skipped []string, err error) {
	if err := os.MkdirAll(dir, 0o755); err != nil { //nolint:gosec // a scaffolded project directory the user browses and edits
		return nil, nil, err
	}

	write := func(name string, content []byte) error {
		target := filepath.Join(dir, name)
		if !force {
			if _, err := os.Stat(target); err == nil {
				skipped = append(skipped, target)
				return nil
			}
		}
		mode := fs.FileMode(0o644)
		if name == "http-client.private.env.json" {
			mode = 0o600
		}
		if err := os.WriteFile(target, content, mode); err != nil {
			return err
		}
		written = append(written, target)
		return nil
	}

	err = fs.WalkDir(projectFS, "project", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix(path, "project/")
		if d.IsDir() {
			if path == "project" {
				return nil
			}
			return os.MkdirAll(filepath.Join(dir, filepath.FromSlash(rel)), 0o755) //nolint:gosec // as above: scaffolded project directory
		}
		content, err := fs.ReadFile(projectFS, path)
		if err != nil {
			return err
		}
		return write(filepath.FromSlash(rel), content)
	})
	if err != nil {
		return nil, nil, err
	}

	envJSON := fmt.Sprintf(`{
  "local": {
    "baseUrl": "http://localhost:%d"
  }
}
`, port)
	if err := write("http-client.env.json", []byte(envJSON)); err != nil {
		return nil, nil, err
	}

	return written, skipped, nil
}

type todo struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Done  bool   `json:"done"`
}

// job is a background task whose state advances on every poll, so a request
// file can show "wait until done" without a real queue behind it.
type job struct {
	ID    string `json:"id"`
	State string `json:"state"`
	polls int
}

// New returns the demo API as an http.Handler.
//
// Routes (bearer means `Authorization: Bearer <anything>`, as `login` returns):
//
//	POST /auth/login                        {"user","password"} -> {"access_token"}
//	GET  /me                                bearer
//	GET  /basic-auth/{user}/{password}      HTTP basic auth
//	POST /oauth/token                       client-credentials grant
//	GET  /keyed                             X-Api-Key: demo-key
//	GET  /todos?done=&page=&limit=          bearer; X-Total-Count and Link headers
//	POST /todos                             bearer; 422 with field errors on an empty title
//	GET|PUT|DELETE /todos/{id}              bearer; 404 when unknown
//	POST /jobs, GET /jobs/{id}              bearer; state goes queued -> running -> done, one step per poll
//	POST /upload                            multipart/form-data, echoes the parts
//	POST /graphql                           bearer; {"query","variables"} with a todos(done:) field
//	GET  /reports/daily.csv                 text/csv
//	GET  /slow?ms=                          sleeps, capped at 10 s
//	GET  /status/{code}, GET /redirect      any status; a 302 to /status/200
//	GET  /health                            uptime, no auth
func New() http.Handler {
	mux := http.NewServeMux()
	started := time.Now()

	var mu sync.Mutex
	todos := map[string]*todo{
		"1": {ID: "1", Title: "Buy milk", Done: false},
		"2": {ID: "2", Title: "Read the apic docs", Done: true},
	}
	nextID := 3
	jobs := map[string]*job{}
	nextJob := 1

	mux.HandleFunc("POST /auth/login", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ User, Password string }
		_ = json.NewDecoder(r.Body).Decode(&in)
		if in.User != demoUser || in.Password != demoPassword {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "bad credentials"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"access_token": "mock-token"})
	})

	mux.HandleFunc("GET /me", requireBearer(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"email": demoUser + "@example.com"})
	}))

	mux.HandleFunc("GET /basic-auth/{user}/{password}", func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != r.PathValue("user") || pass != r.PathValue("password") {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "bad credentials"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"authenticated": true})
	})

	mux.HandleFunc("POST /oauth/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.FormValue("client_id") != demoClientID || r.FormValue("client_secret") != demoClientSecret {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid_client"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"access_token": "mock-oauth-token", "expires_in": 3600})
	})

	mux.HandleFunc("GET /keyed", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") != demoAPIKey {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing or bad X-Api-Key"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "key": demoAPIKey})
	})

	// listTodos returns the todos in id order, optionally filtered by done.
	listTodos := func(done *bool) []todo {
		mu.Lock()
		defer mu.Unlock()
		list := make([]todo, 0, len(todos))
		for _, id := range sortedIDs(todos) {
			if done != nil && todos[id].Done != *done {
				continue
			}
			list = append(list, *todos[id])
		}
		return list
	}

	mux.HandleFunc("GET /todos", requireBearer(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		var done *bool
		if v := q.Get("done"); v != "" {
			b, err := strconv.ParseBool(v)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "done must be true or false"})
				return
			}
			done = &b
		}
		page := positiveInt(q.Get("page"), 1)
		limit := positiveInt(q.Get("limit"), defaultPageSize)
		all := listTodos(done)
		start := (page - 1) * limit
		if start > len(all) {
			start = len(all)
		}
		end := start + limit
		if end > len(all) {
			end = len(all)
		}
		w.Header().Set("X-Total-Count", strconv.Itoa(len(all)))
		w.Header().Set("X-Page", strconv.Itoa(page))
		if end < len(all) {
			next := r.URL.Query()
			next.Set("page", strconv.Itoa(page+1))
			next.Set("limit", strconv.Itoa(limit))
			w.Header().Set("Link", fmt.Sprintf(`</todos?%s>; rel="next"`, next.Encode()))
		}
		writeJSON(w, http.StatusOK, all[start:end])
	}))

	mux.HandleFunc("POST /todos", requireBearer(func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Title string }
		_ = json.NewDecoder(r.Body).Decode(&in)
		if strings.TrimSpace(in.Title) == "" {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
				"error":  "validation failed",
				"fields": map[string]string{"title": "must not be empty"},
			})
			return
		}
		mu.Lock()
		id := strconv.Itoa(nextID)
		nextID++
		t := &todo{ID: id, Title: in.Title}
		todos[id] = t
		out := *t
		mu.Unlock()
		writeJSON(w, http.StatusCreated, out)
	}))

	mux.HandleFunc("GET /todos/{id}", requireBearer(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		t, ok := todos[r.PathValue("id")]
		var out todo
		if ok {
			out = *t
		}
		mu.Unlock()
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		writeJSON(w, http.StatusOK, out)
	}))

	mux.HandleFunc("PUT /todos/{id}", requireBearer(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		t, ok := todos[r.PathValue("id")]
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		var in struct {
			Title *string
			Done  *bool
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		if in.Title != nil {
			t.Title = *in.Title
		}
		if in.Done != nil {
			t.Done = *in.Done
		}
		writeJSON(w, http.StatusOK, t)
	}))

	mux.HandleFunc("DELETE /todos/{id}", requireBearer(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		delete(todos, r.PathValue("id"))
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))

	mux.HandleFunc("POST /jobs", requireBearer(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		id := strconv.Itoa(nextJob)
		nextJob++
		j := &job{ID: id, State: "queued"}
		jobs[id] = j
		out := *j
		mu.Unlock()
		w.Header().Set("Location", "/jobs/"+id)
		writeJSON(w, http.StatusAccepted, out)
	}))

	mux.HandleFunc("GET /jobs/{id}", requireBearer(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		j, ok := jobs[r.PathValue("id")]
		var out job
		if ok {
			// The first poll sees it running, the second sees it done, so a
			// request file can demonstrate polling deterministically.
			j.polls++
			switch {
			case j.polls >= 2:
				j.State = "done"
			case j.polls == 1:
				j.State = "running"
			}
			out = *j
		}
		mu.Unlock()
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		writeJSON(w, http.StatusOK, out)
	}))

	mux.HandleFunc("POST /upload", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "expected multipart/form-data: " + err.Error()})
			return
		}
		fields := map[string]string{}
		for k, v := range r.MultipartForm.Value {
			if len(v) > 0 {
				fields[k] = v[0]
			}
		}
		type part struct {
			Field    string `json:"field"`
			Filename string `json:"filename"`
			Size     int64  `json:"size"`
			Type     string `json:"content_type,omitempty"`
		}
		files := []part{}
		for k, hs := range r.MultipartForm.File {
			for _, h := range hs {
				files = append(files, part{Field: k, Filename: h.Filename, Size: h.Size, Type: h.Header.Get("Content-Type")})
			}
		}
		sort.Slice(files, func(i, j int) bool { return files[i].Field < files[j].Field })
		writeJSON(w, http.StatusCreated, map[string]any{"fields": fields, "files": files})
	})

	mux.HandleFunc("POST /graphql", requireBearer(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil || strings.TrimSpace(in.Query) == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"errors": []map[string]string{{"message": "expected a JSON body with a query"}}})
			return
		}
		data := map[string]any{}
		if strings.Contains(in.Query, "todos") {
			var done *bool
			if v, ok := in.Variables["done"].(bool); ok {
				done = &v
			}
			data["todos"] = listTodos(done)
		}
		if strings.Contains(in.Query, "health") {
			data["health"] = "ok"
		}
		if len(data) == 0 {
			writeJSON(w, http.StatusOK, map[string]any{"errors": []map[string]string{{"message": "unknown field: the demo schema has todos(done: Boolean) and health"}}})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": data})
	}))

	mux.HandleFunc("GET /reports/daily.csv", func(w http.ResponseWriter, r *http.Request) {
		all := listTodos(nil)
		done := 0
		for _, t := range all {
			if t.Done {
				done++
			}
		}
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="daily.csv"`)
		fmt.Fprintf(w, "date,total,done,open\n%s,%d,%d,%d\n", time.Now().UTC().Format(time.DateOnly), len(all), done, len(all)-done)
	})

	mux.HandleFunc("GET /slow", func(w http.ResponseWriter, r *http.Request) {
		ms := positiveInt(r.URL.Query().Get("ms"), 1000)
		if ms > maxSlowMillis {
			ms = maxSlowMillis
		}
		select {
		case <-time.After(time.Duration(ms) * time.Millisecond):
		case <-r.Context().Done():
			return
		}
		writeJSON(w, http.StatusOK, map[string]int{"slept_ms": ms})
	})

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"status":         "ok",
			"service":        "apic-demo",
			"uptime_seconds": int(time.Since(started).Seconds()),
		})
	})

	mux.HandleFunc("GET /status/{code}", func(w http.ResponseWriter, r *http.Request) {
		code, err := strconv.Atoi(r.PathValue("code"))
		if err != nil || code < 100 || code > 999 {
			code = http.StatusOK
		}
		writeJSON(w, code, map[string]int{"status": code})
	})

	mux.HandleFunc("GET /redirect", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/status/200", http.StatusFound)
	})

	return mux
}

func requireBearer(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") || auth == "Bearer " {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthenticated"})
			return
		}
		next(w, r)
	}
}

// positiveInt parses a query value as a positive integer, else returns def.
func positiveInt(s string, def int) int {
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		return def
	}
	return n
}

func sortedIDs(todos map[string]*todo) []string {
	ids := make([]string, 0, len(todos))
	for id := range todos {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		a, _ := strconv.Atoi(ids[i])
		b, _ := strconv.Atoi(ids[j])
		return a < b
	})
	return ids
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
