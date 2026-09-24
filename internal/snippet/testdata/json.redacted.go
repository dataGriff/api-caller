package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

func main() {
	req, err := http.NewRequest("POST", "https://api.example.com/todos?list=***&q=***", strings.NewReader("***"))
	if err != nil {
		panic(err)
	}
	req.Header.Add("Content-Type", "***")
	req.Header.Add("Accept", "***")
	req.Header.Add("X-Note", "***")
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
