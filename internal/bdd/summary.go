package bdd

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Summary is the machine-readable outcome of a run, derived from the
// cucumber JSON report.
type Summary struct {
	OK        bool      `json:"ok"`
	Scenarios int       `json:"scenarios"`
	Passed    int       `json:"passed"`
	Failed    int       `json:"failed"`
	Undefined int       `json:"undefined"`
	Failures  []Failure `json:"failures,omitempty"`
}

// Failure describes one failed or undefined step.
type Failure struct {
	Feature  string `json:"feature"`
	Scenario string `json:"scenario"`
	Step     string `json:"step"`
	Status   string `json:"status"`
	Error    string `json:"error,omitempty"`
}

type cukeFeature struct {
	URI      string `json:"uri"`
	Name     string `json:"name"`
	Elements []struct {
		Name  string `json:"name"`
		Type  string `json:"type"`
		Steps []struct {
			Keyword string `json:"keyword"`
			Name    string `json:"name"`
			Result  struct {
				Status       string `json:"status"`
				ErrorMessage string `json:"error_message"`
			} `json:"result"`
		} `json:"steps"`
	} `json:"elements"`
}

// Summarize parses a cucumber JSON report.
func Summarize(report []byte) (*Summary, error) {
	var features []cukeFeature
	if err := json.Unmarshal(report, &features); err != nil {
		return nil, fmt.Errorf("parse cucumber report: %w", err)
	}
	s := &Summary{OK: true}
	for _, f := range features {
		for _, el := range f.Elements {
			if el.Type != "scenario" {
				continue
			}
			s.Scenarios++
			failed := false
			for _, st := range el.Steps {
				switch st.Result.Status {
				case "failed", "undefined", "pending", "ambiguous":
					failed = true
					if st.Result.Status == "undefined" {
						s.Undefined++
					}
					s.Failures = append(s.Failures, Failure{Feature: f.Name, Scenario: el.Name,
						Step: strings.TrimSpace(st.Keyword) + " " + st.Name, Status: st.Result.Status, Error: st.Result.ErrorMessage})
				}
			}
			if failed {
				s.Failed++
			} else {
				s.Passed++
			}
		}
	}
	s.OK = s.Failed == 0 && s.Scenarios > 0
	return s, nil
}
