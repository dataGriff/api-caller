// TOKEN is an OAuth2 access token from https://idp/token
// apic's TLS settings are not carried over: client cert certs/client.pem · ca certs/ca.pem
// the request line asks for HTTP/2, which this client does not require
package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
)

func main() {
	req, err := http.NewRequest("PURGE", "https://cdn.example.com/x", nil)
	if err != nil {
		panic(err)
	}
	req.Header.Set("Authorization", "Bearer "+os.Getenv("TOKEN"))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}
	fmt.Println(resp.Status)
	fmt.Println(string(data))
}
