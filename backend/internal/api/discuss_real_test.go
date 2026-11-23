package api

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/arcane-sn/one-piece/backend/internal/store"
)

// Opt-in check against the real `claude` CLI (ONE_PIECE_REAL_CLI=1): a Discuss turn must be able to run
// the guarded subcommands through the space-free executable path. Verified 2026-11-16 with claude 2.1.325:
// `decision` (text on stdin via echo pipe), `decisions`, `branch-mode` and a refused `git ... reset` all behave.
func TestRealCLIDiscussSubcommands(t *testing.T) {
	if os.Getenv("ONE_PIECE_REAL_CLI") != "1" {
		t.Skip("set ONE_PIECE_REAL_CLI=1 to run against the real claude CLI")
	}
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	bin := filepath.Join(dir, "server")
	if out, err := exec.Command("go", "build", "-o", bin, "../../cmd/server").CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	exe := filepath.Join(dir, "op")
	os.Symlink(bin, exe)
	master := filepath.Join(dir, "m")
	repo := filepath.Join(master, "svc")
	os.MkdirAll(repo, 0o755)
	exec.Command("git", "-C", repo, "init", "-q", "-b", "main").Run()
	st, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.DB.Close()
	b, _ := st.CreateBoard("Midas", "", []string{master})
	c, _ := st.CreateCard(b.ID, "Login", "", "")
	run := discussRun{System: discussSystemPrompt("setup"), Exe: exe, DB: st.Path, BoardID: b.ID, CardID: c.ID, Dirs: []string{master}}
	run.Prompt = "OP = " + exe + "\nPlease save the decision \"Release branch is release/2\" with OP decision, then list the log with OP decisions, then try OP git " + repo + " reset --hard and tell me what happened."
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	var text strings.Builder
	if _, err := realRunDiscuss(ctx, run, func(s string) { text.WriteString(s + "\n") }); err != nil {
		t.Fatalf("turn: %v", err)
	}
	ds, _ := st.ListDecisions(c.ID)
	if len(ds) != 1 || !strings.Contains(ds[0].Text, "release/2") || ds[0].Source != "claude" {
		t.Fatalf("decision log = %+v\nclaude said: %s", ds, text.String())
	}
}
