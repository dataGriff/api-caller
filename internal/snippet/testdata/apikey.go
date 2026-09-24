package main

import (
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

func main() {
	req, err := http.NewRequest("GET", "https://internal.example.com/v1/items", nil)
	if err != nil {
		panic(err)
	}
	q := req.URL.Query()
	q.Add("api_key", "k-123")
	req.URL.RawQuery = q.Encode()
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} // as apic --insecure
	proxy, err := url.Parse("http://proxy.internal:3128")
	if err != nil {
		panic(err)
	}
	transport.Proxy = http.ProxyURL(proxy)
	client := &http.Client{Transport: transport}
	resp, err := client.Do(req)
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
