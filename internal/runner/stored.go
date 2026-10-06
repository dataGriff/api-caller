package runner

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"sort"
	"time"

	"github.com/dataGriff/api-caller/internal/assert"
	"github.com/dataGriff/api-caller/internal/httpfile"
	"github.com/dataGriff/api-caller/internal/selector"
)

// ParseResult reads a result back from the JSON `apic run --json` prints,
// as the response history stores it, so the renderers can show it as
// they show a fresh one. What was masked when it was written stays
// masked; the request's own name for each header is the one stored.
func ParseResult(data []byte) (*Result, error) {
	var s struct {
		OK      bool `json:"ok"`
		Request struct {
			Name        string            `json:"name"`
			File        string            `json:"file"`
			Line        int               `json:"line"`
			Method      string            `json:"method"`
			URL         string            `json:"url"`
			Headers     map[string]string `json:"headers"`
			Body        string            `json:"body"`
			Auth        string            `json:"auth"`
			HTTPVersion string            `json:"http_version"`
		} `json:"request"`
		Response *Response         `json:"response"`
		Captures map[string]string `json:"captures"`
		Asserts  []assert.Result   `json:"asserts"`
		Errors   []string          `json:"errors"`
		Attempts int               `json:"attempts"`
		SavedTo  string            `json:"saved_to"`
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber() // a body's numbers come back as they were written
	if err := dec.Decode(&s); err != nil {
		return nil, err
	}
	q := s.Request
	res := &Result{
		OK: s.OK,
		Request: Resolved{Name: q.Name, File: q.File, Line: q.Line, Method: q.Method, URL: q.URL,
			Body: q.Body, Auth: q.Auth, HTTPVersion: q.HTTPVersion},
		Response: s.Response, Captures: s.Captures, Asserts: s.Asserts, Errors: s.Errors,
		Attempts: s.Attempts, SavedTo: s.SavedTo,
	}
	names := make([]string, 0, len(q.Headers))
	for n := range q.Headers {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		res.Request.Headers = append(res.Request.Headers, httpfile.Header{Name: n, Value: q.Headers[n]})
	}
	if r := s.Response; r != nil {
		body, err := rawBody(r)
		if err != nil {
			return nil, err
		}
		h := http.Header{}
		for k, v := range r.Headers {
			h.Set(k, v)
		}
		res.raw = &selector.Response{Status: r.Status, StatusText: r.StatusText, Headers: h, Body: body,
			Duration: time.Duration(r.DurationMs) * time.Millisecond}
	}
	return res, nil
}

// rawBody turns a Response's Body back into bytes: base64 decoded for a
// binary body, the text for a text one, and compact JSON for a JSON one.
func rawBody(r *Response) ([]byte, error) {
	switch b := r.Body.(type) {
	case nil:
		return nil, nil
	case string:
		if r.BodyEncoding == Base64 {
			return base64.StdEncoding.DecodeString(b)
		}
		return []byte(b), nil
	default:
		return json.Marshal(b)
	}
}
