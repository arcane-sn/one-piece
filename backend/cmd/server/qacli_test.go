package main

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/arcane-sn/one-piece/backend/internal/qa"
	"github.com/arcane-sn/one-piece/backend/internal/store"
)

const okSpec = `import { test, expect } from './helpers'
test('x', async ({ page }) => {
  await qaStep(page, 'Open', async () => { await page.goto('/'); await expect(page).toHaveTitle(/x/) })
})`

type qaFx struct {
	st    *store.Store
	env   func(string) string
	item  store.Item
	run   int64
	paths qa.Paths
	board store.Board
}

func newQAFx(t *testing.T) *qaFx {
	t.Helper()
	path := filepath.Join(t.TempDir(), "t.db")
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.DB.Close() })
	root, _ := filepath.EvalSymlinks(t.TempDir())
	os.MkdirAll(filepath.Join(root, "web"), 0o755)
	b, _ := st.CreateBoard("Midas", "", []string{root})
	if _, err := st.SetQAConfig(b.ID, qa.Config{BaseURL: "http://localhost:5000",
		Services: []qa.Service{{Name: "web", Folder: filepath.Join(root, "web"), Command: "x", Port: 5000, ReadyURL: "http://localhost:5000/"}}}); err != nil {
		t.Fatal(err)
	}
	c, _ := st.CreateCard(b.ID, "Login", "", "")
	it, _ := st.CreateItem(c.ID, "qa", "Login works", "steps")
	run, _ := st.CreateQARun(it.ID, c.ID, b.ID, nil)
	p := qa.Paths{Data: st.Dir}
	p.PrepareWorkspace(b.ID)
	os.MkdirAll(p.Run(run), 0o755)
	os.WriteFile(p.SpecPath(b.ID, it.Ident), []byte(okSpec), 0o644)
	env := func(k string) string {
		return map[string]string{"ONE_PIECE_DB": path, "ONE_PIECE_BOARD": strconv.FormatInt(b.ID, 10), "ONE_PIECE_MODE": "qa",
			"ONE_PIECE_ITEM": strconv.FormatInt(it.ID, 10), "ONE_PIECE_QA_RUN": strconv.FormatInt(run, 10)}[k]
	}
	return &qaFx{st, env, it, run, p, b}
}

// stubExec replaces Playwright: it writes what a run would leave in the attempt folder.
func stubExec(t *testing.T, report, steps string, calls *int) {
	t.Helper()
	old, oldSB := qa.Exec, qa.SandboxAvailable
	qa.SandboxAvailable = func() bool { return false }
	qa.Exec = func(_ context.Context, _ string, env []string, name string, args ...string) ([]byte, error) {
		*calls++
		var dir string
		for _, e := range env {
			if strings.HasPrefix(e, "QA_ATTEMPT_DIR=") {
				dir = strings.TrimPrefix(e, "QA_ATTEMPT_DIR=")
			}
		}
		os.WriteFile(filepath.Join(dir, "result.json"), []byte(report), 0o644)
		os.WriteFile(filepath.Join(dir, "steps.json"), []byte(steps), 0o644)
		os.WriteFile(filepath.Join(dir, "steps", "01-open.png"), []byte("png"), 0o644)
		os.MkdirAll(filepath.Join(dir, "pw-output", "t"), 0o755)
		os.WriteFile(filepath.Join(dir, "pw-output", "t", "video.webm"), []byte("v"), 0o644)
		os.WriteFile(filepath.Join(dir, "pw-output", "t", "trace.zip"), []byte("z"), 0o644)
		return []byte("SECRETTOKEN-123 raw output"), nil
	}
	t.Cleanup(func() { qa.Exec, qa.SandboxAvailable = old, oldSB })
}

const passReport = `{"stats":{"expected":1},"errors":[],"suites":[]}`
const passSteps = `[{"position":1,"title":"Open","status":"passed","screenshot":"steps/01-open.png","durationMs":5}]`

func TestQARunPassSummaryAndEvidence(t *testing.T) {
	f := newQAFx(t)
	calls := 0
	stubExec(t, passReport, passSteps, &calls)
	out, err := runCLI(f.env, "", "qa-run")
	if err != nil || calls != 1 || !strings.Contains(out, "RUN PASSED") || !strings.Contains(out, "01 passed Open") || !strings.Contains(out, "attempt-1/steps/01-open.png") {
		t.Fatalf("out = %q err = %v calls=%d", out, err, calls)
	}
	if strings.Contains(out, "raw output") || strings.Contains(out, "SECRETTOKEN") {
		t.Error("raw process output must never reach Claude")
	}
	for _, f2 := range []string{"trace.zip", "video.webm", "blocked-hosts.json", "class.json"} {
		if _, err := os.Stat(filepath.Join(f.paths.Attempt(f.run, 1), f2)); err != nil {
			t.Errorf("missing %s", f2)
		}
	}
	if r, _ := f.st.GetQARun(f.run); len(r.Steps) != 1 || r.Steps[0].Attempt != 1 {
		t.Errorf("steps = %+v", r.Steps)
	}
}

func TestQARunRefusals(t *testing.T) {
	f := newQAFx(t)
	calls := 0
	stubExec(t, passReport, passSteps, &calls)
	if _, err := runCLI(f.env, "", "qa-run", "--grep", "x"); err == nil || !strings.Contains(err.Error(), "no arguments") {
		t.Errorf("arguments: %v", err)
	}
	wrong := func(k string) string {
		if k == "ONE_PIECE_MODE" {
			return "review"
		}
		return f.env(k)
	}
	if _, err := runCLI(wrong, "", "qa-run"); err == nil {
		t.Error("wrong mode")
	}
	// lint rejects before any browser; it costs a call but not an attempt
	os.WriteFile(f.paths.SpecPath(f.board.ID, f.item.Ident), []byte("import fs from 'fs'\n"+okSpec), 0o644)
	out, err := runCLI(f.env, "", "qa-run")
	if err != nil || calls != 0 || !strings.Contains(out, "rejected") || !strings.Contains(out, "fs") {
		t.Errorf("lint: %q %v calls=%d", out, err, calls)
	}
	// a symlinked spec is refused
	spec := f.paths.SpecPath(f.board.ID, f.item.Ident)
	os.Remove(spec)
	target := filepath.Join(t.TempDir(), "elsewhere.ts")
	os.WriteFile(target, []byte(okSpec), 0o644)
	os.Symlink(target, spec)
	if _, err := runCLI(f.env, "", "qa-run"); err == nil || !strings.Contains(err.Error(), "refusing") || calls != 0 {
		t.Errorf("symlink: %v calls=%d", err, calls)
	}
	os.Remove(spec)
	os.WriteFile(spec, []byte(okSpec), 0o644)
	for i := 0; i < maxQAAttempts; i++ {
		if _, err := runCLI(f.env, "", "qa-run"); err != nil {
			t.Fatalf("attempt %d: %v", i+1, err)
		}
	}
	if _, err := runCLI(f.env, "", "qa-run"); err == nil || !strings.Contains(err.Error(), "limit reached") {
		t.Errorf("fourth attempt: %v", err)
	}
	// a run that is no longer active cannot be driven
	f.st.FinishQARun(f.run, "passed", "", "", "", 3, nil)
	if _, err := runCLI(f.env, "", "qa-run"); err == nil {
		t.Error("finished run")
	}
}

func TestQAScrubsSecretsFromSummary(t *testing.T) {
	f := newQAFx(t)
	os.MkdirAll(f.paths.AuthDir(), 0o700)
	os.WriteFile(f.paths.Auth(f.board.ID), []byte(`{"cookies":[{"name":"s","value":"cookie-value-9999"}]}`), 0o600)
	calls := 0
	stubExec(t, `{"stats":{"unexpected":1},"errors":[],"suites":[{"specs":[{"tests":[{"results":[{"status":"failed","error":{"message":"expected cookie-value-9999 to be visible"}}]}]}]}]}`,
		`[{"position":1,"title":"Open","status":"failed","screenshot":"steps/01-open.png","error":"saw cookie-value-9999"}]`, &calls)
	out, err := runCLI(f.env, "", "qa-run")
	if err != nil || strings.Contains(out, "cookie-value-9999") || !strings.Contains(out, "[redacted]") || !strings.Contains(out, "RUN FAILED") {
		t.Fatalf("out = %q %v", out, err)
	}
	if r, _ := f.st.GetQARun(f.run); strings.Contains(r.Steps[0].Error, "cookie-value-9999") {
		t.Error("stored step error must be scrubbed")
	}
}

func TestQABlockedAndBadSpec(t *testing.T) {
	f := newQAFx(t)
	if _, err := runCLI(f.env, "", "qa-blocked"); err == nil {
		t.Error("a reason is required")
	}
	if out, err := runCLI(f.env, "", "qa-blocked", "login", "page", "shown"); err != nil || !strings.Contains(out, "blocked") {
		t.Fatalf("%q %v", out, err)
	}
	if b, _ := os.ReadFile(filepath.Join(f.paths.Run(f.run), "blocked.txt")); string(b) != "login page shown" {
		t.Errorf("blocked.txt = %q", b)
	}
	calls := 0
	stubExec(t, `{"stats":{},"errors":[{"message":"SyntaxError: bad"}],"suites":[]}`, ``, &calls)
	out, _ := runCLI(f.env, "", "qa-run")
	if !strings.Contains(out, "BROKEN SPEC") {
		t.Errorf("out = %q", out)
	}
}
