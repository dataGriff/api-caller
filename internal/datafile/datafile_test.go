package datafile

import (
	"strings"
	"testing"
)

func TestCSVAndJSON(t *testing.T) {
	rows, err := Read([]byte("\ufeffid, name\n1,\"Smith, J\"\n2,  x\n"), "rows.csv")
	if err != nil || len(rows) != 2 || rows[0].Values["name"] != "Smith, J" || rows[1].Values["name"] != "x" || rows[0].String() != "id=1 name=Smith, J" {
		t.Fatalf("csv: %+v %v", rows, err)
	}
	rows, err = Read([]byte(`[{"id": 7, "big": 12345678901, "ok": true, "tags": ["a"], "none": null, "s": "x"}]`), "-")
	if err != nil || len(rows) != 1 || rows[0].String() != `big=12345678901 id=7 none= ok=true s=x tags=["a"]` {
		t.Fatalf("json: %+v %v", rows, err)
	}
	for data, want := range map[string]string{
		"":              "empty",
		"a,,b\n1,2,3\n": "column 2",
		"a,b\n1,2,3\n":  "wrong number of fields",
		`[1, 2]`:        "array of objects",
		`{"id": 1}`:     "",
	} {
		name := "x.csv"
		if strings.HasPrefix(data, "[") || strings.HasPrefix(data, "{") {
			name = "x.json"
		}
		_, err := Read([]byte(data), name)
		if want == "" {
			if err == nil {
				t.Errorf("%q should fail", data)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v, want %q", data, err, want)
		}
	}
}
