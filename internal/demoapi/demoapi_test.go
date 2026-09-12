package demoapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStatusOutOfRangeFallsBackTo200(t *testing.T) {
	srv := httptest.NewServer(New())
	defer srv.Close()

	res, err := http.Get(srv.URL + "/status/700")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Fatalf("status code = %d, want %d", res.StatusCode, http.StatusOK)
	}
}
