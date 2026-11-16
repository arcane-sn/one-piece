package qa

import (
	"encoding/json"
	"regexp"
	"strings"
)

// Step is one qaStep of an attempt, written by helpers.ts into steps.json.
type Step struct {
	Position      int    `json:"position"`
	Title         string `json:"title"`
	Status        string `json:"status"` // passed | failed
	Screenshot    string `json:"screenshot"`
	DurationMs    int    `json:"durationMs"`
	Error         string `json:"error"`
	LoginRedirect bool   `json:"loginRedirect"`
}

// Attempt is what one Playwright run produced.
type Attempt struct {
	Steps      []Step
	Ran        int      // tests that ran
	Failed     int      // tests that failed
	TopErrors  []string // errors outside any test: compile errors, no tests found
	FirstError string   // first error of a failing test
}

// ParseAttempt reads Playwright's JSON report and the steps.json the helper wrote. Missing or broken
// input yields an Attempt with a top-level error, never a panic.
func ParseAttempt(report, steps []byte) Attempt {
	var a Attempt
	var rep struct {
		Stats struct {
			Expected, Unexpected, Flaky int
		} `json:"stats"`
		Errors []struct{ Message string } `json:"errors"`
		Suites []json.RawMessage          `json:"suites"`
	}
	if err := json.Unmarshal(report, &rep); err != nil {
		a.TopErrors = append(a.TopErrors, "Playwright produced no readable result")
	} else {
		a.Ran = rep.Stats.Expected + rep.Stats.Unexpected + rep.Stats.Flaky
		a.Failed = rep.Stats.Unexpected
		for _, e := range rep.Errors {
			a.TopErrors = append(a.TopErrors, firstLines(e.Message))
		}
		for _, s := range rep.Suites {
			if a.FirstError == "" {
				a.FirstError = firstTestError(s)
			}
		}
	}
	json.Unmarshal(steps, &a.Steps)
	return a
}

func firstTestError(raw json.RawMessage) string {
	var n struct {
		Suites []json.RawMessage `json:"suites"`
		Specs  []struct {
			Tests []struct {
				Results []struct {
					Status string
					Error  struct{ Message string }
					Errors []struct{ Message string }
				}
			}
		}
	}
	if json.Unmarshal(raw, &n) != nil {
		return ""
	}
	for _, sp := range n.Specs {
		for _, t := range sp.Tests {
			for _, r := range t.Results {
				if r.Status != "passed" && r.Status != "skipped" {
					if r.Error.Message != "" {
						return firstLines(r.Error.Message)
					}
					for _, e := range r.Errors {
						return firstLines(e.Message)
					}
				}
			}
		}
	}
	for _, s := range n.Suites {
		if e := firstTestError(s); e != "" {
			return e
		}
	}
	return ""
}

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func firstLines(s string) string {
	s = ansiRe.ReplaceAllString(s, "")
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > 6 {
		lines = lines[:6]
	}
	out := strings.Join(lines, "\n")
	if len(out) > 500 {
		out = out[:500] + "…"
	}
	return out
}

// Passed reports a clean run: at least one test, none failed, no top-level error, no failed step.
func (a Attempt) Passed() bool {
	if a.Ran == 0 || a.Failed > 0 || len(a.TopErrors) > 0 {
		return false
	}
	for _, s := range a.Steps {
		if s.Status != "passed" {
			return false
		}
	}
	return len(a.Steps) > 0
}

// Class is how a finished attempt is read.
type Class struct {
	Kind   string // passed | assertion | spec | infra
	Reason string // for spec and infra: the message the owner sees
}

var infraErr = regexp.MustCompile(`net::ERR_(CONNECTION_REFUSED|NAME_NOT_RESOLVED|BLOCKED_BY_CLIENT|PROXY_CONNECTION_FAILED|TUNNEL_CONNECTION_FAILED|CONNECTION_RESET|CONNECTION_TIMED_OUT|ADDRESS_UNREACHABLE)`)

// Classify tells a product finding from a spec or environment problem. blocked are the hosts the proxy refused.
func Classify(a Attempt, blocked []string) Class {
	for _, s := range a.Steps {
		if s.LoginRedirect {
			return Class{"infra", "Login needed: the browser was sent to the login page."}
		}
	}
	switch {
	case len(a.TopErrors) > 0:
		return Class{"spec", "Broken spec: " + a.TopErrors[0]}
	case a.Ran == 0:
		return Class{"spec", "Broken spec: no test was found in the spec."}
	case a.Passed():
		return Class{Kind: "passed"}
	}
	msg := a.FirstError
	for _, s := range a.Steps {
		msg += "\n" + s.Error
	}
	if m := infraErr.FindString(msg); m != "" {
		return Class{"infra", "The page could not be reached (" + m + ")."}
	}
	if len(blocked) > 0 {
		return Class{"infra", "The page tried to reach hosts outside the allowed list (" + strings.Join(blocked, ", ") + "). Add them to Allowed hosts if the page needs them."}
	}
	return Class{Kind: "assertion"}
}

var evidenceRe = regexp.MustCompile(`^(steps/[0-9]{2,3}-[a-z0-9-]{1,60}\.png|trace\.zip|video\.webm|result\.json|steps\.json|repos\.json|blocked-hosts\.json)$`)

// ValidEvidenceFile reports whether name is one of the files the evidence route may serve.
func ValidEvidenceFile(name string) bool { return evidenceRe.MatchString(name) }
