package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/arcane-sn/one-piece/backend/internal/qa"
	"github.com/arcane-sn/one-piece/backend/internal/store"
)

// qaCommands only work in a browser QA session (ONE_PIECE_MODE=qa; the server sets the item and run in the environment).
var qaCommands = map[string]bool{"qa-run": true, "qa-blocked": true}

const (
	maxQAAttempts = 3 // attempts that reach Playwright
	maxQACalls    = 8 // qa-run calls in total, lint rejections included
)

// runQACLI runs qa-run (one Playwright attempt of the item's spec; takes NO arguments, so Claude cannot
// change what runs) and qa-blocked <reason…>. Everything printed is composed here: Claude never sees raw process output.
//
//	qa-run                 lint the spec, run it once with the server-generated config, print a summary
//	qa-blocked <reason…>   declare an environment problem Claude cannot repair; the server blocks the item
func runQACLI(args []string, getenv func(string) string, st *store.Store, boardID int64, in io.Reader, out io.Writer) error {
	if getenv("ONE_PIECE_MODE") != "qa" {
		return errors.New(args[0] + " is only available in a browser QA session")
	}
	itemID, err1 := strconv.ParseInt(getenv("ONE_PIECE_ITEM"), 10, 64)
	runID, err2 := strconv.ParseInt(getenv("ONE_PIECE_QA_RUN"), 10, 64)
	if err1 != nil || err2 != nil {
		return errors.New("ONE_PIECE_ITEM and ONE_PIECE_QA_RUN must be set")
	}
	it, err := st.GetItem(itemID)
	if err != nil || it.Kind != "qa" {
		return errors.New("QA test case not found")
	}
	card, err := st.GetCard(it.CardID)
	if err != nil || card.BoardID != boardID {
		return errors.New("QA test case not found")
	}
	run, err := st.GetQARun(runID)
	if err != nil || run.ItemID != itemID || run.Status != "running" {
		return errors.New("this QA run is not active")
	}
	paths := qa.Paths{Data: st.Dir}
	runDir := paths.Run(runID)

	if args[0] == "qa-blocked" {
		reason := strings.TrimSpace(strings.Join(args[1:], " "))
		if reason == "" {
			return errors.New("usage: qa-blocked <reason>")
		}
		if len(reason) > 300 {
			reason = reason[:300]
		}
		if err := os.WriteFile(filepath.Join(runDir, "blocked.txt"), []byte(qa.Scrub(reason, secretsFor(paths, boardID))), 0o600); err != nil {
			return err
		}
		fmt.Fprintln(out, "noted; the item will be blocked")
		return nil
	}
	if len(args) != 1 {
		return errors.New("qa-run takes no arguments")
	}

	calls := bumpCalls(runDir)
	if calls > maxQACalls {
		return errors.New("limit reached; report your verdict or use qa-blocked")
	}
	n := countAttempts(runDir)
	if n >= maxQAAttempts {
		return errors.New("limit reached; report your verdict or use qa-blocked")
	}
	cfg, err := st.GetQAConfig(boardID)
	if err != nil || !cfg.Configured() {
		return errors.New("this board has no QA stack")
	}
	src, err := os.ReadFile(paths.SpecPath(boardID, it.Ident))
	if err != nil {
		return errors.New("the spec file does not exist")
	}
	if bad := qa.LintSpec(string(src)); len(bad) > 0 {
		fmt.Fprintln(out, "The spec was rejected before running (no browser was started, no attempt used):")
		for _, b := range bad {
			fmt.Fprintln(out, "- "+b)
		}
		return nil
	}
	if err := paths.CheckSpecFile(boardID, it.Ident); err != nil {
		return fmt.Errorf("refusing to run: %v", err)
	}

	n++
	attemptDir := paths.Attempt(runID, n)
	if err := os.MkdirAll(filepath.Join(attemptDir, "steps"), 0o755); err != nil {
		return err
	}
	proxy, err := qa.NewProxy(cfg.AllowedHosts)
	if err != nil {
		return err
	}
	defer proxy.Close()
	cfgDir := filepath.Join(paths.Runner(), "configs")
	os.MkdirAll(cfgDir, 0o755)
	cfgFile := filepath.Join(cfgDir, fmt.Sprintf("run-%d-%d.config.ts", runID, n))
	defer os.Remove(cfgFile)
	if err := os.WriteFile(cfgFile, []byte(paths.PlaywrightConfig(boardID, it.Ident, attemptDir, proxy.Addr(), cfg)), 0o600); err != nil {
		return err
	}
	board, err := st.GetBoard(boardID)
	if err != nil {
		return err
	}
	_, _, perr := paths.RunPlaywright(context.Background(), cfgFile, attemptDir, paths.AttemptEnv(attemptDir, cfg), board.Folders)
	qa.CollectArtifacts(attemptDir)

	blocked := mergeRouteBlocked(proxy.Blocked(), filepath.Join(attemptDir, "blocked-routes.txt"))
	writeJSONFile(filepath.Join(attemptDir, qa.BlockedFile), blocked)
	report, _ := os.ReadFile(filepath.Join(attemptDir, qa.ResultFile))
	stepsRaw, _ := os.ReadFile(filepath.Join(attemptDir, qa.StepsFile))
	secrets := secretsFor(paths, boardID)
	a := qa.ParseAttempt(report, stepsRaw)
	if perr != nil && len(report) == 0 && len(a.TopErrors) == 0 {
		a.TopErrors = append(a.TopErrors, "Playwright could not be started")
	}
	for i := range a.Steps {
		a.Steps[i].Error = qa.Scrub(a.Steps[i].Error, secrets)
	}
	a.FirstError = qa.Scrub(a.FirstError, secrets)
	for i := range a.TopErrors {
		a.TopErrors[i] = qa.Scrub(a.TopErrors[i], secrets)
	}
	if err := st.AddQASteps(runID, n, a.Steps); err != nil {
		return err
	}
	cl := qa.Classify(a, blocked)
	writeJSONFile(filepath.Join(attemptDir, "class.json"), cl)
	fmt.Fprint(out, attemptSummary(n, a, cl, blocked, attemptDir))
	return nil
}

func secretsFor(p qa.Paths, boardID int64) []string {
	b, err := os.ReadFile(p.Auth(boardID))
	if err != nil {
		return nil
	}
	return qa.SecretsFromState(b)
}

func attemptSummary(n int, a qa.Attempt, cl qa.Class, blocked []string, dir string) string {
	var b strings.Builder
	label := map[string]string{"passed": "RUN PASSED", "assertion": "RUN FAILED (an assertion or step failed)", "spec": "BROKEN SPEC", "infra": "ENVIRONMENT PROBLEM"}[cl.Kind]
	fmt.Fprintf(&b, "Attempt %d of %d: %s\n", n, maxQAAttempts, label)
	if cl.Reason != "" {
		fmt.Fprintf(&b, "%s\n", cl.Reason)
	}
	for _, s := range a.Steps {
		shot := ""
		if s.Screenshot != "" {
			shot = "  " + filepath.Join(dir, s.Screenshot)
		}
		fmt.Fprintf(&b, "%02d %s %s%s\n", s.Position, s.Status, s.Title, shot)
	}
	if a.FirstError != "" {
		fmt.Fprintf(&b, "First error:\n%s\n", a.FirstError)
	}
	if len(blocked) > 0 {
		fmt.Fprintf(&b, "Blocked hosts: %s\n", strings.Join(blocked, ", "))
	}
	switch cl.Kind {
	case "passed":
		b.WriteString("Now open EVERY screenshot above with Read and compare it with the expected result before you give your verdict.\n")
	case "assertion":
		b.WriteString("Open the screenshots with Read. If the page really differs from the expected result, that is a finding (verdict fail). If your selector or timing was wrong, repair the spec and run qa-run again.\n")
	case "spec":
		b.WriteString("Repair your spec and run qa-run again, or use qa-blocked if you cannot.\n")
	case "infra":
		b.WriteString("This looks like an environment problem, not a product bug. Use qa-blocked with a short reason unless you can see it is your spec.\n")
	}
	return b.String()
}

func countAttempts(runDir string) int {
	n := 0
	for {
		if _, err := os.Stat(filepath.Join(runDir, fmt.Sprintf("attempt-%d", n+1))); err != nil {
			return n
		}
		n++
	}
}

// bumpCalls counts qa-run calls in the run folder (the run's own counter, which Claude cannot reset).
func bumpCalls(runDir string) int {
	f := filepath.Join(runDir, "calls")
	b, _ := os.ReadFile(f)
	n, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	n++
	os.WriteFile(f, []byte(strconv.Itoa(n)), 0o600)
	return n
}

func writeJSONFile(path string, v any) {
	b, _ := jsonMarshal(v)
	os.WriteFile(path, b, 0o644)
}

var jsonMarshal = json.Marshal

// mergeRouteBlocked adds the hosts the page-side route fixture aborted (it acts before the proxy sees them).
func mergeRouteBlocked(hosts []string, file string) []string {
	b, err := os.ReadFile(file)
	if err != nil {
		return hosts
	}
	seen := map[string]bool{}
	for _, h := range hosts {
		seen[h] = true
	}
	for _, h := range strings.Fields(string(b)) {
		if !seen[h] {
			seen[h] = true
			hosts = append(hosts, h)
		}
	}
	sort.Strings(hosts)
	return hosts
}
