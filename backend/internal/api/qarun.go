package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/arcane-sn/one-piece/backend/internal/gitx"
	"github.com/arcane-sn/one-piece/backend/internal/qa"
	"github.com/arcane-sn/one-piece/backend/internal/store"
)

const qaBrowserSystemPrompt = `You are a QA engineer. You check ONE test case in a real browser by writing ONE Playwright spec, running it, and judging the result together with its screenshots.
Rules:
- The first message names the one spec file you may write and the helpers file. Read the helpers file first. Import only from './helpers' (test, expect, qaStep). Write one qaStep(page, '<step title>', async () => { ... }) per step of the test case, and assert every expected result with expect(...). Never type credentials: the browser is already logged in. Never navigate outside the base URL.
- Run the spec with the qa-run subcommand named in the first message. It takes no arguments and prints a summary with the path of a screenshot for every step.
- If the run failed because of your own spec (a wrong selector, timing, a broken import), repair the spec and run it again. You have a small fixed number of runs. Never weaken an assertion, remove a step, or change an expected result to get a pass.
- After a run, open EVERY screenshot of the final attempt with Read and compare it with the expected result. Pass only when the run passed AND the screenshots show the expected result.
- If the page differs from the expected result, that is a finding: report fail. If the problem is the environment (a login page, an error page, a missing service) and not the product code, run qa-blocked with a short reason instead of reporting fail.
- You cannot edit product code, run other commands, or read anything outside the listed folders.
Your final message must be ONLY one JSON object and nothing else:
{"result":"pass"|"fail","issue":"one sentence (max 140 characters) naming what is wrong; empty with pass","desc":"with fail: what the page shows now, what the test case expects, and the screenshot step involved, so a developer can fix it; with pass: one or two sentences of evidence"}
Write in the same language as the test case.`

type qaSessionIn struct {
	System, Prompt, Exe, DB string
	BoardID, ItemID, RunID  int64
	ReadDirs                []string // product folders, the workspace, this run's evidence
	Spec                    string   // the one file Claude may edit
	DenyEdit                []string // product folders
	DenyRead                []string // login, database, runner, secrets
}

// agentQA runs the browser QA session. It is a variable so tests can stub it.
//
// Verified against the real CLI (claude 2.1.325, 2026-11-16, see qa_real_test.go):
//   - Edit(//<abs file>) lets Write/Edit change that one file; every other path is refused; Write(...) rules are
//     rejected by the CLI (only Edit rules exist)
//   - a bare Read/Grep/Glob reads anywhere the OS user can, so QA denies the login, database, runner and
//     secrets explicitly: --disallowedTools "Read(//dir/**)" blocks Read, Grep and Glob inside it
//   - --strict-mcp-config removes MCP tools; --setting-sources project keeps personal allow rules out
//   - only Bash(<exe> qa-run:*) and Bash(<exe> qa-blocked:*) are allowed, so there is no shell
var agentQA = func(ctx context.Context, in qaSessionIn, onLog func(string)) (string, error) {
	tmp, err := os.MkdirTemp("", "one-piece-qa-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	args := []string{"-p", "--output-format", "stream-json", "--verbose", "--no-session-persistence", "--max-turns", "40",
		"--setting-sources", "project", "--strict-mcp-config", "--permission-mode", "default", "--system-prompt", in.System}
	for _, d := range in.ReadDirs {
		args = append(args, "--add-dir", d)
	}
	args = append(args, "--allowedTools", "Read", "Grep", "Glob", "Edit(/"+in.Spec+")",
		"Bash("+in.Exe+" qa-run:*)", "Bash("+in.Exe+" qa-blocked:*)", "--disallowedTools")
	for _, d := range in.DenyEdit {
		args = append(args, "Edit(/"+d+"/**)")
	}
	for _, d := range in.DenyRead {
		args = append(args, "Read(/"+d+"/**)")
	}
	args = append(args, "Read(/"+in.DB+"*)", "Read(~/.ssh/**)", "Read(~/.aws/**)", "Read(~/.config/**)", "Read(~/.claude/**)", "Read(**/.env*)")
	env := []string{"ONE_PIECE_DB=" + in.DB, "ONE_PIECE_BOARD=" + strconv.FormatInt(in.BoardID, 10), "ONE_PIECE_MODE=qa",
		"ONE_PIECE_ITEM=" + strconv.FormatInt(in.ItemID, 10), "ONE_PIECE_QA_RUN=" + strconv.FormatInt(in.RunID, 10)}
	return runStreamEnv(ctx, args, tmp, in.Prompt, env, onLog)
}

// Test seams: nothing here starts a process in tests.
var (
	qaStartStack = qa.StartStack
	qaSnapshot   = qa.Snapshot
	// qaLiveCheck visits the session check URL with the saved login (headless unless the board is headed).
	qaLiveCheck = func(ctx context.Context, p qa.Paths, boardID int64, cfg qa.Config) (bool, string) {
		env := []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME"), "TMPDIR=" + os.TempDir(), "PLAYWRIGHT_BROWSERS_PATH=" + p.Browsers()}
		hosts, _ := json.Marshal(cfg.AllowedHosts)
		env = append(env, "QA_ALLOWED_HOSTS="+string(hosts))
		headed := "0"
		if cfg.Headed {
			headed = "1"
		}
		cctx, cancel := context.WithTimeout(ctx, time.Minute)
		defer cancel()
		out, err := qa.Exec(cctx, p.Runner(), env, "node", filepath.Join(p.Runner(), "check.mjs"), cfg.SessionCheckURL, p.Auth(boardID), headed)
		var v struct {
			FinalURL string `json:"finalUrl"`
			Status   int    `json:"status"`
			Error    string `json:"error"`
		}
		if i := strings.LastIndex(string(out), "{"); err != nil || i < 0 || json.Unmarshal(out[i:], &v) != nil {
			return false, "the session check could not run"
		}
		if v.Error != "" {
			return false, "the session check could not load the page: " + shorten(v.Error, 120)
		}
		if v.Status == 401 || v.Status == 403 {
			return false, "Login needed"
		}
		if cfg.LoginURL != cfg.SessionCheckURL && cfg.LoginURL != cfg.BaseURL && strings.HasPrefix(v.FinalURL, cfg.LoginURL) {
			return false, "Login needed"
		}
		return true, ""
	}
)

func countAttemptDirs(runDir string) int {
	n := 0
	for {
		if _, err := os.Stat(filepath.Join(runDir, fmt.Sprintf("attempt-%d", n+1))); err != nil {
			return n
		}
		n++
	}
}

// qaRepoRoots are the repos a QA run records and guards: the ticket's repos plus each service folder's repo.
func qaRepoRoots(b store.Board, ticket []store.Branch, cfg qa.Config) []string {
	seen := map[string]bool{}
	var out []string
	add := func(p string) {
		if p != "" && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	for _, r := range ticket {
		add(r.Repo)
	}
	for _, sv := range cfg.Services {
		for _, f := range b.Folders {
			if root, err := gitx.RepoRootWithin(f, sv.Folder); err == nil {
				add(root)
				break
			}
		}
	}
	return out
}

func (s *server) snapshots(repos []string) []qa.RepoState {
	var out []qa.RepoState
	for _, r := range repos {
		if st, err := qaSnapshot(r); err == nil {
			out = append(out, st)
		}
	}
	return out
}

// executeQA runs a QA Test Case in a real browser (the board has a QA stack) and applies the outcome rules of the spec.
func (s *server) executeQA(ctx context.Context, it store.Item, c store.Card, b store.Board, cfg qa.Config) runOutcome {
	logLine := func(l string) { s.st.AppendItemLog(it.ID, l) }
	p := s.qaPaths()
	if !s.qaTryLock("run") {
		logLine("Blocked: Another QA run is in progress.")
		s.st.SetItemStatus(it.ID, "todo")
		return runBlocked
	}
	defer s.qaUnlock()

	runID, err := s.st.CreateQARun(it.ID, c.ID, b.ID, nil)
	if err != nil {
		logLine("Failed: " + err.Error())
		s.st.SetItemStatus(it.ID, "todo")
		return runBlocked
	}
	runDir := p.Run(runID)
	attemptsOf := func() int { return countAttemptDirs(runDir) }
	finish := func(status, reason, issue, summary string, hosts []string) {
		s.st.FinishQARun(runID, status, reason, issue, summary, attemptsOf(), hosts)
		for _, id := range prunedRuns(s.st, it.ID) {
			os.RemoveAll(p.Run(id))
		}
	}
	block := func(reason string) runOutcome {
		logLine("Blocked: " + reason)
		finish("blocked", reason, "", "", nil)
		s.st.SetItemStatus(it.ID, "todo")
		return runBlocked
	}
	stopped := func() runOutcome {
		logLine("Stopped")
		finish("stopped", "", "", "", nil)
		s.st.SetItemStatus(it.ID, "todo")
		return runBlocked
	}
	logLine(fmt.Sprintf("QA run #%d (browser)", runID))

	if !p.ToolchainReady() {
		return block("Browser QA is not installed. Run `server qa-setup` once.")
	}
	ticket, _ := s.ticketRepos(c.ID)
	if err := checkOnFeature(ticket); err != nil {
		return block(err.Error())
	}
	roots := qaRepoRoots(b, ticket, cfg)
	before := s.snapshots(roots)
	s.st.SetQARunRepos(runID, before)

	if raw, err := os.ReadFile(p.Auth(b.ID)); err != nil || qa.StateExpired(raw, time.Now().Unix()) {
		return block("Login needed. Open Edit Board and use Log in for QA.")
	}
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return block(err.Error())
	}
	if err := p.PrepareWorkspace(b.ID); err != nil {
		return block("Could not prepare the QA workspace: " + err.Error())
	}
	if err := p.EnsureSpecFile(b.ID, it.Ident); err != nil {
		return block(err.Error())
	}
	writeJSONFile(filepath.Join(runDir, "repos.json"), before)

	stack, err := qaStartStack(ctx, cfg.Services, filepath.Join(runDir, "services"), p.StackFile())
	if err != nil {
		if ctx.Err() != nil {
			return stopped()
		}
		return block(err.Error())
	}
	stackUp := true
	stopStack := func() {
		if stackUp {
			stack.Stop()
			stackUp = false
		}
	}
	defer stopStack()
	if ok, why := qaLiveCheck(ctx, p, b.ID, cfg); !ok {
		if ctx.Err() != nil {
			return stopped()
		}
		return block(why + ". Open Edit Board and use Log in for QA.")
	}

	exe, err := agentExe()
	if err != nil {
		return block(err.Error())
	}
	reads := map[string]bool{}
	sessionLog := func(l string) {
		logLine(l)
		if n := strings.TrimPrefix(l, "Reading "); n != l {
			reads[n] = true
		}
	}
	var msg strings.Builder
	fmt.Fprintf(&msg, "Product: %s\n", b.Name)
	if b.Description != "" {
		fmt.Fprintf(&msg, "About the product: %s\n", b.Description)
	}
	fmt.Fprintf(&msg, "\nBusiness ticket: %s %s\n%s\n", c.Ident, c.Title, c.Description)
	if blk := decisionBlock(s.st, c.ID); blk != "" {
		msg.WriteString("\n" + blk)
	}
	fmt.Fprintf(&msg, "\nYour QA Test Case: %s %s\n%s\n", it.Ident, it.Title, it.Body)
	fmt.Fprintf(&msg, "\nBase URL (the QA stack is already running and logged in): %s\n", cfg.BaseURL)
	fmt.Fprintf(&msg, "The one spec file you may write: %s\nHelpers (read first): %s\n", p.SpecPath(b.ID, it.Ident), filepath.Join(p.Workspace(b.ID), "helpers.ts"))
	fmt.Fprintf(&msg, "Commands: %s qa-run (no arguments) | %s qa-blocked <reason>\nEvidence of this run lives under: %s\n", exe, exe, runDir)
	msg.WriteString("\nRepos under test (the code that is running):\n")
	for _, r := range before {
		fmt.Fprintf(&msg, "- %s on %s at %s%s\n", r.Repo, r.Branch, shortSHA(r.SHA), map[bool]string{true: " (uncommitted changes)"}[r.Dirty])
	}
	fmt.Fprintf(&msg, "\nProduct folders you may read:\n- %s\n", strings.Join(b.Folders, "\n- "))
	reads2 := append(append([]string{}, b.Folders...), p.Workspace(b.ID), runDir)
	final, err := agentQA(ctx, qaSessionIn{System: qaBrowserSystemPrompt, Prompt: msg.String(), Exe: exe, DB: s.st.Path,
		BoardID: b.ID, ItemID: it.ID, RunID: runID, ReadDirs: reads2, Spec: p.SpecPath(b.ID, it.Ident),
		DenyEdit: b.Folders, DenyRead: []string{p.AuthDir(), p.Runner()}}, sessionLog)
	stopStack()
	if errors.Is(ctx.Err(), context.Canceled) {
		return stopped()
	}
	if err != nil {
		return block("Claude could not finish the QA session: " + shorten(err.Error(), 200))
	}
	// backstop: a QA run must leave every product repo exactly as it found it
	if changed := qa.Changed(before, s.snapshots(roots)); len(changed) > 0 {
		return block(fmt.Sprintf("QA changed %s; no verdict was recorded. Check that repo.", filepath.Base(changed[0])))
	}

	if raw, err := os.ReadFile(filepath.Join(runDir, "blocked.txt")); err == nil {
		return block(string(raw))
	}
	n := attemptsOf()
	if n == 0 {
		return block("Broken spec: the spec never got through the checks to a browser run.")
	}
	attemptDir := p.Attempt(runID, n)
	var cl qa.Class
	rawCl, _ := os.ReadFile(filepath.Join(attemptDir, "class.json"))
	json.Unmarshal(rawCl, &cl)
	var hosts []string
	rawH, _ := os.ReadFile(filepath.Join(attemptDir, qa.BlockedFile))
	json.Unmarshal(rawH, &hosts)
	if cl.Kind == "spec" || cl.Kind == "infra" || cl.Kind == "" {
		reason := cl.Reason
		if reason == "" {
			reason = "The run left no result."
		}
		return block(reason)
	}
	res, issue, desc, perr := parseQA(final)
	if perr != nil {
		return block("Claude's answer could not be read. Try again.")
	}
	run, _ := s.st.GetQARun(runID)
	for _, st := range run.Steps {
		if st.Attempt == n && st.Screenshot != "" && !reads[filepath.Base(st.Screenshot)] {
			return block("Claude did not look at the screenshots of the last attempt, so no verdict was recorded.")
		}
	}
	switch {
	case res == "pass" && cl.Kind == "passed":
		finish("passed", "", "", desc, hosts)
		return s.passQA(it, logLine, desc)
	case res == "pass":
		return block("The verdict (pass) disagrees with the run, which failed. Run it again.")
	default: // fail: a finding, whether the run failed on an assertion or the screenshots did not match
		finish("failed", "", issue, desc, hosts)
		return s.failQA(it, logLine, issue, desc)
	}
}

func shortSHA(s string) string {
	if len(s) > 8 {
		return s[:8]
	}
	return s
}

func prunedRuns(st *store.Store, itemID int64) []int64 {
	ids, _ := st.PruneQARuns(itemID)
	return ids
}

func writeJSONFile(path string, v any) {
	b, _ := json.Marshal(v)
	os.WriteFile(path, b, 0o644)
}
