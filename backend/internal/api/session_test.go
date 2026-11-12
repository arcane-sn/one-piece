package api

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeClaude puts a `claude` shell script first in PATH. It records its arguments and ONE_PIECE_* env.
func fakeClaude(t *testing.T, body string) (argsFile, envFile string) {
	t.Helper()
	dir := t.TempDir()
	argsFile, envFile = filepath.Join(dir, "args"), filepath.Join(dir, "env")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + argsFile + "\nenv | grep '^ONE_PIECE_' > " + envFile + "\n" + body
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	return
}

func TestRunSessionParsesStream(t *testing.T) {
	argsFile, envFile := fakeClaude(t, `cat > /dev/null
echo '{"type":"system","session_id":"sess-1"}'
echo '{"type":"assistant","message":{"content":[{"type":"text","text":"hello"}]}}'
echo '{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Read"},{"type":"text","text":"world"}]}}'
echo '{"type":"result","is_error":false,"result":"ok","session_id":"sess-1"}'
`)
	var texts []string
	var announced string
	sid, err := runSession(context.Background(), sessionSpec{
		System: "SYS", Prompt: "hi", SessionID: "old-9", Dirs: []string{"/a", "/b"},
		AllowedTools: []string{"Read", "Bash(/x/op git:*)"}, Env: []string{"ONE_PIECE_CARD=42"},
		OnSession: func(id string) { announced = id },
	}, func(s string) { texts = append(texts, s) })
	if err != nil || sid != "sess-1" || announced != "sess-1" {
		t.Fatalf("sid=%q announced=%q err=%v", sid, announced, err)
	}
	if strings.Join(texts, "|") != "hello|world" {
		t.Errorf("texts = %v", texts)
	}
	a, _ := os.ReadFile(argsFile)
	got := string(a)
	for _, want := range []string{"--system-prompt\nSYS", "--resume\nold-9", "--add-dir\n/a", "--add-dir\n/b", "--allowedTools\nRead\nBash(/x/op git:*)", "--max-turns\n12"} {
		if !strings.Contains(got, want) {
			t.Errorf("args lack %q:\n%s", want, got)
		}
	}
	if e, _ := os.ReadFile(envFile); !strings.Contains(string(e), "ONE_PIECE_CARD=42") {
		t.Errorf("env = %q", e)
	}
	// no session id → no --resume
	fakeClaude(t, `cat > /dev/null
echo '{"type":"result","is_error":false,"result":"ok","session_id":"s2"}'
`)
	if sid, err := runSession(context.Background(), sessionSpec{Prompt: "x"}, func(string) {}); err != nil || sid != "s2" {
		t.Fatalf("fresh: %q %v", sid, err)
	}
}

func TestRunSessionErrorAndCancel(t *testing.T) {
	fakeClaude(t, "cat > /dev/null\necho 'No conversation found' >&2\nexit 1\n")
	if _, err := runSession(context.Background(), sessionSpec{Prompt: "x"}, func(string) {}); err == nil || !strings.Contains(err.Error(), "No conversation found") {
		t.Fatalf("error = %v", err)
	}
	fakeClaude(t, "exec sleep 5\n")
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := runSession(ctx, sessionSpec{Prompt: "x"}, func(string) {})
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 3*time.Second {
		t.Fatalf("cancel: %v after %v", err, time.Since(start))
	}
}
