package qa

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	commentRe  = regexp.MustCompile(`(?m)(^|\s)//.*$`)
	blockCmtRe = regexp.MustCompile(`(?s)/\*.*?\*/`)
	importRe   = regexp.MustCompile(`(?s)\bimport\b[^;'"]*?(?:from\s*)?['"]([^'"]+)['"]`)
	dynImport  = regexp.MustCompile(`\bimport\s*\(`)
	bannedRe   = regexp.MustCompile(`\b(require|eval|Function|process|globalThis|child_process|XMLHttpRequest|WebSocket|fetch|chromium|firefox|webkit|newContext|newBrowserContext|launch|launchPersistentContext|connectOverCDP)\b`)
)

// LintSpec checks a Claude-written spec before it runs. It is a guard against mistakes and against text
// injected through a page, not a security boundary against a determined author (the sandbox is).
func LintSpec(src string) []string {
	var bad []string
	if n := strings.Count(src, "\n") + 1; n > 400 {
		bad = append(bad, fmt.Sprintf("the spec has %d lines; the limit is 400", n))
	}
	code := commentRe.ReplaceAllString(blockCmtRe.ReplaceAllString(src, " "), "$1")
	for _, m := range importRe.FindAllStringSubmatch(code, -1) {
		if m[1] != "./helpers" {
			bad = append(bad, fmt.Sprintf("import of %q is not allowed; import only from './helpers'", m[1]))
		}
	}
	if dynImport.MatchString(code) {
		bad = append(bad, "dynamic import() is not allowed")
	}
	seen := map[string]bool{}
	for _, loc := range bannedRe.FindAllStringIndex(code, -1) {
		m := code[loc[0]:loc[1]]
		// a word inside a URL path ("/v1/binding/process/ok-1") is data, not code
		if (loc[0] > 0 && code[loc[0]-1] == '/') || (loc[1] < len(code) && code[loc[1]] == '/') {
			continue
		}
		if !seen[m] {
			seen[m] = true
			bad = append(bad, fmt.Sprintf("%q is not allowed in a QA spec", m))
		}
	}
	if !strings.Contains(code, "qaStep(") {
		bad = append(bad, "use qaStep(page, 'title', async () => { ... }) for every step")
	}
	if !strings.Contains(code, "expect(") {
		bad = append(bad, "assert the expected result with expect(...)")
	}
	return bad
}
