// Command server runs the fake API that examples/mock's .http files talk
// to, so apic can be tried with no network access.
package main

import (
	"log"
	"net/http"

	"github.com/dataGriff/api-caller/internal/mockserver"
)

func main() {
	const addr = ":8089"
	log.Printf("mock api listening on http://localhost%s", addr)
	log.Fatal(http.ListenAndServe(addr, mockserver.New()))
}
