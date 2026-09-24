// sign this request with AWS Signature V4 (service execute-api, region eu-west-2); apic does it with the AWS credential chain
package main

import (
	"fmt"
	"io"
	"net/http"
)

func main() {
	req, err := http.NewRequest("GET", "https://abc.execute-api.eu-west-2.amazonaws.com/prod/orders", nil)
	if err != nil {
		panic(err)
	}
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
