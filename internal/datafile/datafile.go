// Package datafile reads the rows of a data-driven run, `apic run --data`:
// a CSV file whose header row names the columns, or a JSON array of
// objects. Each row becomes the variables of one iteration.
package datafile

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

// Row is one iteration's variables, with the column order kept for
// display.
type Row struct {
	Names  []string
	Values map[string]string
}

// String renders the row as name=value pairs, in column order.
func (r Row) String() string {
	parts := make([]string, 0, len(r.Names))
	for _, n := range r.Names {
		parts = append(parts, n+"="+r.Values[n])
	}
	return strings.Join(parts, " ")
}

// Read parses rows from data; name (the file's name, or "-" for stdin)
// picks the format by extension, and anything but .csv that starts with
// `[` is read as JSON.
func Read(data []byte, name string) ([]Row, error) {
	trimmed := bytes.TrimSpace(data)
	if strings.HasSuffix(strings.ToLower(name), ".json") || (!strings.HasSuffix(strings.ToLower(name), ".csv") && len(trimmed) > 0 && trimmed[0] == '[') {
		return readJSON(trimmed)
	}
	return readCSV(data)
}

func readCSV(data []byte) ([]Row, error) {
	r := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(data, []byte("\ufeff")))) // a spreadsheet's byte order mark
	r.TrimLeadingSpace = true
	header, err := r.Read()
	if errors.Is(err, io.EOF) {
		return nil, errors.New("the file is empty; the first row names the variables")
	}
	if err != nil {
		return nil, fmt.Errorf("csv: %w", err)
	}
	for i, h := range header {
		header[i] = strings.TrimSpace(h)
		if header[i] == "" {
			return nil, fmt.Errorf("csv: column %d of the header row has no name", i+1)
		}
	}
	var rows []Row
	for {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("csv: %w", err)
		}
		row := Row{Names: header, Values: map[string]string{}}
		for i, v := range rec {
			row.Values[header[i]] = v
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func readJSON(data []byte) ([]Row, error) {
	var raw []map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("json: want an array of objects: %w", err)
	}
	rows := make([]Row, 0, len(raw))
	for _, obj := range raw {
		row := Row{Values: map[string]string{}}
		for k, v := range obj {
			row.Names = append(row.Names, k)
			row.Values[k] = stringify(v)
		}
		// JSON objects have no order; the names are sorted so the
		// heading of an iteration is stable.
		sort.Strings(row.Names)
		rows = append(rows, row)
	}
	return rows, nil
}

// stringify renders a JSON value as a variable: strings as they are,
// numbers without an exponent, objects and arrays as JSON.
func stringify(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(t)
	case nil:
		return ""
	}
	b, _ := json.Marshal(v)
	return string(b)
}
