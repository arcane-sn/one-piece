package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/arcane-sn/one-piece/backend/internal/store"
)

// streamClaude runs `claude -p` with read-only tools on dirs and reports progress lines
// through onLog; it returns Claude's final message. It is a variable so tests can stub it.
var streamClaude = func(ctx context.Context, system, user string, dirs []string, onLog func(string)) (string, error) {
	tmp, err := os.MkdirTemp("", "one-piece-claude-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	args := []string{"-p", "--output-format", "stream-json", "--verbose", "--no-session-persistence",
		"--setting-sources", "project", "--allowedTools", "Read,Grep,Glob", "--system-prompt", system}
	for _, d := range dirs {
		args = append(args, "--add-dir", d)
	}
	return runStream(ctx, args, tmp, user, onLog)
}

// runStream runs `claude` with args in cwd, feeding user on stdin and turning the
// stream-json events into onLog lines. It returns Claude's final message.
func runStream(ctx context.Context, args []string, cwd, user string, onLog func(string)) (string, error) {
	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Dir = cwd
	cmd.Stdin = strings.NewReader(user)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	if err := cmd.Start(); err != nil {
		return "", err
	}
	var final string
	var isErr bool
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		var ev struct {
			Type    string `json:"type"`
			IsError bool   `json:"is_error"`
			Result  string `json:"result"`
			Message struct {
				Content []struct {
					Type  string         `json:"type"`
					Text  string         `json:"text"`
					Name  string         `json:"name"`
					Input map[string]any `json:"input"`
				} `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(sc.Bytes(), &ev) != nil {
			continue
		}
		switch ev.Type {
		case "assistant":
			for _, c := range ev.Message.Content {
				if c.Type == "tool_use" {
					onLog(describeTool(c.Name, c.Input))
				} else if t := strings.TrimSpace(c.Text); c.Type == "text" && t != "" && !strings.HasPrefix(t, "{") {
					onLog("Claude: " + shorten(t, 140))
				}
			}
		case "result":
			final, isErr = ev.Result, ev.IsError
		}
	}
	werr := cmd.Wait()
	if isErr || (final == "" && werr != nil) {
		return "", fmt.Errorf("claude: %v: %s", werr, shorten(strings.TrimSpace(stderr.String()+" "+final), 200))
	}
	return final, nil
}

func describeTool(name string, in map[string]any) string {
	str := func(k string) string { v, _ := in[k].(string); return v }
	switch name {
	case "Read":
		return "Reading " + filepath.Base(str("file_path"))
	case "Grep":
		return "Searching for " + shorten(str("pattern"), 60)
	case "Glob":
		return "Listing " + shorten(str("pattern"), 60)
	case "Edit", "Write":
		return "Editing " + filepath.Base(str("file_path"))
	}
	return "Using " + name
}

func shorten(s string, n int) string {
	r := []rune(strings.Join(strings.Fields(s), " "))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n]) + "…"
}

// GET /api/cards/{id}/review → latest review, or null.
func (s *server) getReview(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	if _, err := s.st.GetCard(id); err != nil {
		writeErr(w, err)
		return
	}
	rv, err := s.st.LatestReview(id)
	if errors.Is(err, store.ErrNotFound) {
		writeJSON(w, http.StatusOK, json.RawMessage("null"))
		return
	}
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rv)
}

// POST /api/cards/{id}/approve {"approved": bool}
func (s *server) approveCard(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	var in struct {
		Approved bool `json:"approved"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	c, err := s.st.SetApproved(id, in.Approved)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}
