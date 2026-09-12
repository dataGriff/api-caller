package template

import (
	"errors"
	"testing"
)

func TestRender(t *testing.T) {
	vars := map[string]string{"a": "1", "b": "2"}
	r := func(e string) (string, bool, error) {
		if e == "boom" {
			return "", false, errors.New("bad")
		}
		v, ok := vars[e]
		return v, ok, nil
	}
	got, err := Render("x{{a}}y{{ b }}z", r)
	if err != nil || got != "x1y2z" {
		t.Fatalf("%q %v", got, err)
	}
	got, err = Render("{{a}}{{c}}{{d}}{{c}}", r)
	var me *MissingError
	if !errors.As(err, &me) || len(me.Exprs) != 2 || got != "1{{c}}{{d}}{{c}}" {
		t.Fatalf("%q %v", got, err)
	}
	if _, err = Render("{{boom}}", r); err == nil {
		t.Fatal("want error")
	}
	if e := Exprs("{{a}} {{b}} {{a}}"); len(e) != 2 {
		t.Fatal(e)
	}
}
