package cli

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
)

func frame(body string) string {
	return fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(body), body)
}

func TestLSPCommandServesOverStdio(t *testing.T) {
	app := New()
	var stdout, stderr bytes.Buffer
	app.Stdin = strings.NewReader(
		frame(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"rootUri":null,"capabilities":{}}}`) +
			frame(`{"jsonrpc":"2.0","id":2,"method":"shutdown"}`) +
			frame(`{"jsonrpc":"2.0","method":"exit"}`))
	app.Stdout, app.Stderr = &stdout, &stderr
	if code := app.Execute(context.Background(), []string{"-C", t.TempDir(), "lsp"}); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, `"name":"apic"`) || !strings.Contains(out, `"hoverProvider":true`) || !strings.Contains(out, `"id":2,"result":null`) {
		t.Errorf("stdout:\n%s", out)
	}
}

func TestLSPCommandFailsWithoutShutdown(t *testing.T) {
	app := New()
	var stdout, stderr bytes.Buffer
	app.Stdin = strings.NewReader(frame(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`))
	app.Stdout, app.Stderr = &stdout, &stderr
	if code := app.Execute(context.Background(), []string{"lsp"}); code != 1 || !strings.Contains(stderr.String(), "without a shutdown") {
		t.Errorf("exit %d: %s", code, stderr.String())
	}
}
