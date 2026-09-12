// Package mockserver is a tiny, fully in-memory HTTP API used by
// examples/mock and its test suite, so apic's features can be exercised
// end to end with no network access.
package mockserver

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
)

const (
	demoUser         = "alice"
	demoPassword     = "s3cret"
	demoClientID     = "demo-client"
	demoClientSecret = "demo-secret"
)

type todo struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Done  bool   `json:"done"`
}

// New returns the mock API as an http.Handler.
func New() http.Handler {
	mux := http.NewServeMux()

	var mu sync.Mutex
	todos := map[string]*todo{"1": {ID: "1", Title: "Buy milk", Done: false}}
	nextID := 2

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

	mux.HandleFunc("GET /todos", requireBearer(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		list := make([]*todo, 0, len(todos))
		for _, id := range sortedIDs(todos) {
			list = append(list, todos[id])
		}
		writeJSON(w, http.StatusOK, list)
	}))

	mux.HandleFunc("POST /todos", requireBearer(func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Title string }
		_ = json.NewDecoder(r.Body).Decode(&in)
		mu.Lock()
		id := strconv.Itoa(nextID)
		nextID++
		t := &todo{ID: id, Title: in.Title}
		todos[id] = t
		mu.Unlock()
		writeJSON(w, http.StatusCreated, t)
	}))

	mux.HandleFunc("GET /todos/{id}", requireBearer(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		t, ok := todos[r.PathValue("id")]
		mu.Unlock()
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		writeJSON(w, http.StatusOK, t)
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

	mux.HandleFunc("GET /status/{code}", func(w http.ResponseWriter, r *http.Request) {
		code, err := strconv.Atoi(r.PathValue("code"))
		if err != nil {
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
