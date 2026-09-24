package main

import (
	"fmt"
	"io"
	"net/http"
	"strings"
)

func main() {
	req, err := http.NewRequest("POST", "https://api.example.com/todos?list=home&q=it's", strings.NewReader("{\"title\": \"café \\\"x\\\"\",\n \"done\": false}"))
	if err != nil {
		panic(err)
	}
	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("Accept", "application/json")
	req.Header.Add("X-Note", "say \"hi\" $HOME")
	req.Header.Set("Authorization", "Bearer s3cret-token")
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
