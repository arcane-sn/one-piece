package qa

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Exec starts a process and returns its combined output. A variable so tests never need Playwright.
var Exec = func(ctx context.Context, dir string, env []string, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = env
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	err := cmd.Run()
	return buf.Bytes(), err
}

// SandboxAvailable reports whether sandbox-exec exists (macOS).
var SandboxAvailable = func() bool { _, err := exec.LookPath("sandbox-exec"); return err == nil }

// Attempt files, relative to an attempt folder.
const (
	ResultFile  = "result.json"
	StepsFile   = "steps.json"
	BlockedFile = "blocked-hosts.json"
)

// RunPlaywright runs one attempt of one spec: the proxy is already up, the config already written.
// The process runs under sandbox-exec when available (no writes outside the attempt folder and the temp
// folders, none at all under the product folders). It returns the combined output (kept out of Claude's sight).
func (p Paths) RunPlaywright(ctx context.Context, cfgFile, attemptDir string, env []string, productFolders []string) ([]byte, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 6*time.Minute)
	defer cancel()
	args := []string{p.PlaywrightBin(), "test", "--config", cfgFile}
	name := "node"
	// the playwright "bin" is a node script; run it with node so the profile covers one known binary
	cmdArgs := args
	sandboxed := false
	if SandboxAvailable() {
		prof := filepath.Join(attemptDir, "sandbox.sb")
		if err := os.WriteFile(prof, []byte(SandboxProfile(attemptDir, productFolders)), 0o600); err == nil {
			cmdArgs = append([]string{"-f", prof, name}, args...)
			name = "sandbox-exec"
			sandboxed = true
		}
	}
	out, err := Exec(ctx, p.Runner(), env, name, cmdArgs...)
	return out, sandboxed, err
}

// CollectArtifacts moves trace.zip and video.webm out of Playwright's output folder to the attempt root.
func CollectArtifacts(attemptDir string) {
	filepath.Walk(filepath.Join(attemptDir, "pw-output"), func(path string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() {
			return nil
		}
		switch {
		case fi.Name() == "trace.zip" || strings.HasSuffix(fi.Name(), ".zip") && strings.Contains(path, "trace"):
			os.Rename(path, filepath.Join(attemptDir, "trace.zip"))
		case strings.HasSuffix(fi.Name(), ".webm"):
			os.Rename(path, filepath.Join(attemptDir, "video.webm"))
		}
		return nil
	})
}
