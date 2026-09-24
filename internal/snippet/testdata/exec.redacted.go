// TOKEN is the output of: gcloud auth print-access-token
package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
)

func main() {
	req, err := http.NewRequest("GET", "https://api.example.com/me", nil)
	if err != nil {
		panic(err)
	}
	req.Header.Set("X-Token", os.Getenv("TOKEN"))
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
