package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/arcane-sn/one-piece/backend/internal/qa"
)

// Opt-in end-to-end check with a real browser: ONE_PIECE_REAL_QA=<runner dir installed by `server qa-setup`>.
func TestRealPlaywrightAttempt(t *testing.T) {
	runner := os.Getenv("ONE_PIECE_REAL_QA")
	if runner == "" {
		t.Skip("set ONE_PIECE_REAL_QA to a runner folder installed by `server qa-setup`")
	}
	app := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<html><head><title>Sample Shop</title></head><body><h1 id=h>Dashboard</h1>
<script>if(location.search)fetch('http://tracker.example.org/x').catch(()=>{})</script></body></html>`)
	}))
	defer app.Close()
	f := newQAFx(t)
	port := app.Listener.Addr().(interface{ String() string }).String()
	port = port[strings.LastIndex(port, ":")+1:]
	pn, _ := strconv.Atoi(port)
	cfg, _ := f.st.GetQAConfig(f.board.ID)
	cfg.BaseURL = app.URL
	cfg.Services[0].Port, cfg.Services[0].ReadyURL = pn, app.URL+"/"
	if _, err := f.st.SetQAConfig(f.board.ID, cfg); err != nil {
		t.Fatal(err)
	}
	os.RemoveAll(f.paths.Runner())
	os.Symlink(runner, f.paths.Runner())
	f.paths.PrepareWorkspace(f.board.ID)
	os.MkdirAll(f.paths.AuthDir(), 0o700)
	os.WriteFile(f.paths.Auth(f.board.ID), []byte(`{"cookies":[],"origins":[]}`), 0o600)
	spec := f.paths.SpecPath(f.board.ID, f.item.Ident)

	write := func(s string) { os.WriteFile(spec, []byte(s), 0o644) }
	write(`import { test, expect, qaStep } from './helpers'
test('dashboard', async ({ page }) => {
  await qaStep(page, 'Open the app', async () => { await page.goto('/?t=1'); await expect(page).toHaveTitle('Sample Shop') })
  await qaStep(page, 'See the dashboard', async () => { await expect(page.locator('#h')).toHaveText('Dashboard') })
})`)
	out, err := runCLI(f.env, "", "qa-run")
	t.Logf("pass attempt:\n%s", out)
	if err != nil || !strings.Contains(out, "RUN PASSED") || !strings.Contains(out, "02 passed See the dashboard") {
		t.Fatalf("passing spec: %v", err)
	}
	ad := f.paths.Attempt(f.run, 1)
	for _, e := range []string{"steps/01-open-the-app.png", "steps/02-see-the-dashboard.png", "trace.zip", "video.webm", "result.json", "steps.json"} {
		if fi, err := os.Stat(filepath.Join(ad, e)); err != nil || fi.Size() == 0 {
			t.Errorf("evidence %s missing", e)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(ad, "blocked-hosts.json")); !strings.Contains(string(b), "tracker.example.org") {
		t.Errorf("the page's request to a non-allowed host must be blocked and recorded: %s", b)
	}

	write(`import { test, expect, qaStep } from './helpers'
test('dashboard', async ({ page }) => {
  await qaStep(page, 'Wrong heading', async () => { await page.goto('/'); await expect(page.locator('#h'), 'heading').toHaveText('Settings', { timeout: 2000 }) })
})`)
	out, _ = runCLI(f.env, "", "qa-run")
	t.Logf("fail attempt:\n%s", out)
	if !strings.Contains(out, "RUN FAILED") || !strings.Contains(out, "02") && !strings.Contains(out, "01 failed Wrong heading") {
		t.Errorf("failing spec: %s", out)
	}
	if _, err := os.Stat(filepath.Join(f.paths.Attempt(f.run, 2), "steps", "01-wrong-heading.png")); err != nil {
		t.Error("a failing step must still have its screenshot")
	}

	// the sandbox profile: no writes under a product folder, writes in the attempt folder are fine
	if qa.SandboxAvailable() {
		prod := t.TempDir()
		prof := filepath.Join(t.TempDir(), "p.sb")
		os.WriteFile(prof, []byte(qa.SandboxProfile(ad, []string{prod})), 0o600)
		if b, err := exec.Command("sandbox-exec", "-f", prof, "sh", "-c", "echo x > "+filepath.Join(prod, "f")).CombinedOutput(); err == nil {
			t.Errorf("a write under a product folder must fail: %s", b)
		}
		if b, err := exec.Command("sandbox-exec", "-f", prof, "sh", "-c", "echo x > "+filepath.Join(ad, "ok")).CombinedOutput(); err != nil {
			t.Errorf("a write in the attempt folder must work: %v %s", err, b)
		}
	}
}
