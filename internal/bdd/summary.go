package bdd

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Summary is the machine-readable outcome of a run, derived from the
// cucumber JSON report.
type Summary struct {
	OK        bool      `json:"ok"` // no scenario failed; check Scenarios for how many ran
	Scenarios int       `json:"scenarios"`
	Passed    int       `json:"passed"`
	Failed    int       `json:"failed"`
	Skipped   int       `json:"skipped"` // not run, e.g. after --stop-on-failure
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
		// godog emits a background element before each scenario it applies to;
		// its failures belong to that scenario.
		var pending []Failure
		for _, el := range f.Elements {
			if el.Type == "background" {
				for _, st := range el.Steps {
					switch st.Result.Status {
					case "failed", "undefined", "pending", "ambiguous":
						pending = append(pending, Failure{Feature: f.Name, Scenario: "", Step: strings.TrimSpace(st.Keyword) + " " + st.Name, Status: st.Result.Status, Error: st.Result.ErrorMessage})
					}
				}
				continue
			}
			if el.Type != "scenario" {
				continue
			}
			s.Scenarios++
			failed := len(pending) > 0
			for _, pf := range pending {
				pf.Scenario = el.Name
				if pf.Status == "undefined" {
					s.Undefined++
				}
				s.Failures = append(s.Failures, pf)
			}
			pending = nil
			ran := false
			for _, st := range el.Steps {
				switch st.Result.Status {
				case "failed", "undefined", "pending", "ambiguous":
					failed = true
					if st.Result.Status == "undefined" {
						s.Undefined++
					}
					s.Failures = append(s.Failures, Failure{Feature: f.Name, Scenario: el.Name,
						Step: strings.TrimSpace(st.Keyword) + " " + st.Name, Status: st.Result.Status, Error: st.Result.ErrorMessage})
				case "passed":
					ran = true
				}
			}
			switch {
			case failed:
				s.Failed++
			case !ran && len(el.Steps) > 0:
				s.Skipped++ // every step skipped: the scenario never ran
			default:
				s.Passed++
			}
		}
	}
	s.OK = s.Failed == 0 // an empty selection (e.g. a tag matching nothing) is not a failure; Scenarios says how many ran
	return s, nil
}
