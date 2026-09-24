package main

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
)

func main() {
	var buf bytes.Buffer
	form := multipart.NewWriter(&buf)
	if err := form.WriteField("title", "Q3 report"); err != nil {
		panic(err)
	}
	{
		data, err := os.ReadFile("docs/report.pdf")
		if err != nil {
			panic(err)
		}
		h := textproto.MIMEHeader{}
		h.Set("Content-Disposition", "form-data; name=\"file\"; filename=\"q3.pdf\"")
		h.Set("Content-Type", "application/pdf")
		part, err := form.CreatePart(h)
		if err != nil {
			panic(err)
		}
		if _, err := part.Write(data); err != nil {
			panic(err)
		}
	}
	if err := form.Close(); err != nil {
		panic(err)
	}
	req, err := http.NewRequest("POST", "http://localhost:8080/upload", &buf)
	if err != nil {
		panic(err)
	}
	req.Header.Set("Content-Type", form.FormDataContentType())
	req.SetBasicAuth("alice", "pa'ss")
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
