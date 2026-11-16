package qa

import (
	"embed"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

//go:embed assets/*
var assets embed.FS

// Paths are the folders QA uses, all under the data directory (the folder of the database file).
type Paths struct{ Data string }

func (p Paths) Root() string     { return filepath.Join(p.Data, "qa") }
func (p Paths) Runner() string   { return filepath.Join(p.Root(), "runner") }
func (p Paths) Browsers() string { return filepath.Join(p.Runner(), "browsers") }
func (p Paths) Workspace(b int64) string {
	return filepath.Join(p.Root(), "workspace", fmt.Sprintf("board-%d", b))
}
func (p Paths) Specs(b int64) string { return filepath.Join(p.Workspace(b), "specs") }
func (p Paths) AuthDir() string      { return filepath.Join(p.Root(), "auth") }
func (p Paths) Auth(b int64) string {
	return filepath.Join(p.AuthDir(), fmt.Sprintf("board-%d.json", b))
}
func (p Paths) Runs() string        { return filepath.Join(p.Root(), "runs") }
func (p Paths) Run(id int64) string { return filepath.Join(p.Runs(), fmt.Sprintf("%d", id)) }
func (p Paths) Attempt(id int64, n int) string {
	return filepath.Join(p.Run(id), fmt.Sprintf("attempt-%d", n))
}
func (p Paths) StackFile() string { return filepath.Join(p.Root(), "stack.json") }

// ToolchainReady reports whether `server qa-setup` has installed Playwright.
func (p Paths) ToolchainReady() bool {
	_, err := os.Stat(filepath.Join(p.Runner(), "node_modules", ".bin", "playwright"))
	return err == nil
}

func (p Paths) PlaywrightBin() string {
	return filepath.Join(p.Runner(), "node_modules", ".bin", "playwright")
}

// Setup installs the runner (npm install, then the Chromium build) and writes the static scripts.
func Setup(p Paths, out func(string)) error {
	if err := p.WriteRunnerFiles(); err != nil {
		return err
	}
	for _, c := range [][]string{{"npm", "install", "--no-audit", "--no-fund"}, {"npx", "playwright", "install", "chromium"}} {
		out("$ " + strings.Join(c, " "))
		cmd := exec.Command(c[0], c[1:]...)
		cmd.Dir = p.Runner()
		cmd.Env = append(os.Environ(), "PLAYWRIGHT_BROWSERS_PATH="+p.Browsers())
		b, err := cmd.CombinedOutput()
		out(string(b))
		if err != nil {
			return err
		}
	}
	return nil
}

// WriteRunnerFiles writes package.json, login.mjs and check.mjs into the runner folder.
func (p Paths) WriteRunnerFiles() error {
	if err := os.MkdirAll(p.Runner(), 0o755); err != nil {
		return err
	}
	for _, n := range []string{"package.json", "login.mjs", "check.mjs"} {
		b, _ := assets.ReadFile("assets/" + n)
		if err := os.WriteFile(filepath.Join(p.Runner(), n), b, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// PrepareWorkspace creates a board's workspace: specs/helpers.ts (read-only; Claude may edit only its one spec) and a node_modules link
// to the runner, so specs resolve @playwright/test. The specs folder is created empty.
func (p Paths) PrepareWorkspace(boardID int64) error {
	ws := p.Workspace(boardID)
	if err := os.MkdirAll(p.Specs(boardID), 0o755); err != nil {
		return err
	}
	b, _ := assets.ReadFile("assets/helpers.ts")
	if err := os.WriteFile(filepath.Join(p.Specs(boardID), "helpers.ts"), b, 0o444); err != nil {
		// a read-only copy from an earlier run: replace it
		os.Remove(filepath.Join(p.Specs(boardID), "helpers.ts"))
		if err := os.WriteFile(filepath.Join(p.Specs(boardID), "helpers.ts"), b, 0o444); err != nil {
			return err
		}
	}
	link := filepath.Join(ws, "node_modules")
	if _, err := os.Lstat(link); err != nil {
		return os.Symlink(filepath.Join(p.Runner(), "node_modules"), link)
	}
	return nil
}

// SpecPath is the one file Claude may write for a QA item.
func (p Paths) SpecPath(boardID int64, ident string) string {
	return filepath.Join(p.Specs(boardID), ident+".spec.ts")
}

// EnsureSpecFile makes sure the item's spec exists as a regular file (never a symlink).
func (p Paths) EnsureSpecFile(boardID int64, ident string) error {
	f := p.SpecPath(boardID, ident)
	if fi, err := os.Lstat(f); err == nil {
		if !fi.Mode().IsRegular() {
			return fmt.Errorf("%s is not a regular file", f)
		}
		return nil
	}
	return os.WriteFile(f, nil, 0o644)
}

// CheckSpecFile refuses a symlinked spec or specs folder (checked right before Playwright starts).
func (p Paths) CheckSpecFile(boardID int64, ident string) error {
	for _, d := range []string{p.Specs(boardID), p.SpecPath(boardID, ident)} {
		fi, err := os.Lstat(d)
		if err != nil {
			return err
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%s is a symlink", d)
		}
	}
	if fi, _ := os.Lstat(p.SpecPath(boardID, ident)); !fi.Mode().IsRegular() {
		return fmt.Errorf("the spec is not a regular file")
	}
	return nil
}

// AttemptEnv is the environment of one Playwright attempt. Nothing of the server's own environment leaks in.
func (p Paths) AttemptEnv(attemptDir string, cfg Config) []string {
	hosts, _ := json.Marshal(cfg.AllowedHosts)
	env := []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME"), "TMPDIR=" + os.TempDir(), "LANG=en_US.UTF-8",
		"PLAYWRIGHT_BROWSERS_PATH=" + p.Browsers(), "QA_ATTEMPT_DIR=" + attemptDir, "QA_ALLOWED_HOSTS=" + string(hosts)}
	if loginDiffers(cfg) {
		env = append(env, "QA_LOGIN_URL="+cfg.LoginURL)
	}
	return env
}

// loginDiffers: a login redirect can only be told apart when the login URL is not the base URL itself.
func loginDiffers(c Config) bool {
	l, e1 := url.Parse(c.LoginURL)
	b, e2 := url.Parse(c.BaseURL)
	return e1 == nil && e2 == nil && c.LoginURL != "" && (l.Host != b.Host || strings.TrimRight(l.Path, "/") != strings.TrimRight(b.Path, "/"))
}

// PlaywrightConfig renders the per-attempt config: one spec, one worker, no retries, the board's proxy and login.
// Claude cannot change any of it.
func (p Paths) PlaywrightConfig(boardID int64, ident, attemptDir, proxyAddr string, cfg Config) string {
	q := func(s string) string { b, _ := json.Marshal(s); return string(b) }
	return fmt.Sprintf(`import { defineConfig } from '@playwright/test'
export default defineConfig({
  testDir: %s,
  testMatch: %s,
  workers: 1,
  retries: 0,
  timeout: 120000,
  outputDir: %s,
  reporter: [['json', { outputFile: %s }]],
  use: {
    baseURL: %s,
    storageState: %s,
    proxy: { server: %s },
    trace: 'on',
    video: 'on',
    serviceWorkers: 'block',
    headless: %v,
    viewport: { width: 1280, height: 800 },
  },
})
`, q(p.Specs(boardID)), q(ident+".spec.ts"), q(filepath.Join(attemptDir, "pw-output")), q(filepath.Join(attemptDir, "result.json")),
		q(cfg.BaseURL), q(p.Auth(boardID)), q("http://"+proxyAddr), !cfg.Headed)
}

// SandboxProfile denies every file write except the attempt's folder and the temp folders, and denies all
// writes under the product folders: the OS-level guarantee that a spec cannot modify a product repo.
func SandboxProfile(attemptDir string, productFolders []string) string {
	var b strings.Builder
	b.WriteString("(version 1)\n(allow default)\n(deny file-write*)\n")
	b.WriteString(fmt.Sprintf("(allow file-write* (subpath %q) (subpath \"/private/var/folders\") (subpath \"/private/tmp\") (subpath \"/dev\"))\n", attemptDir))
	for _, f := range productFolders {
		if r, err := filepath.EvalSymlinks(f); err == nil { // seatbelt matches resolved paths
			f = r
		}
		b.WriteString(fmt.Sprintf("(deny file-write* (subpath %q))\n", f))
	}
	return b.String()
}
