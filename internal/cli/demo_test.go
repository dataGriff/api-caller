package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestDemoRejectsZeroPort(t *testing.T) {
	app := New()
	var stdout, stderr bytes.Buffer
	app.Stdout = &stdout
	app.Stderr = &stderr

	if code := app.Execute(context.Background(), []string{"demo", "--port", "0"}); code != 2 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "--port must be between 1 and 65535") {
		t.Fatalf("unexpected stderr: %s", stderr.String())
	}
}
