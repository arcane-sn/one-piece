package api

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/arcane-sn/one-piece/backend/internal/store"
)

const descriptionSystemPrompt = `You help a product owner write a Jira-style ticket description in Markdown.
Write only the description: no preamble, no surrounding code fence.
Use these sections: "## Summary", "## Context", "## Requirements" (short bullets),
"## Definition of Done" (a checklist of testable items written as "- [ ] ..."), and "## Out of Scope".
Keep the ticket small and manageable: one focused change that a developer can finish in a day or two.
If the idea is bigger than that, describe only the smallest valuable first piece and list the rest under "Out of Scope".
Write in the same language as the owner's idea. If an existing description is provided, improve it and keep its intent.`

// runClaude asks `claude -p` for a completion without tools or project settings.
// It is a variable so tests can stub the CLI.
var runClaude = func(ctx context.Context, system, user string) (string, error) {
	dir, err := os.MkdirTemp("", "one-piece-claude-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	cmd := exec.CommandContext(ctx, "claude", "-p", "--tools", "", "--no-session-persistence",
		"--setting-sources", "project", "--system-prompt", system)
	cmd.Dir = dir // empty dir: nothing project-specific gets loaded
	cmd.Stdin = strings.NewReader(user)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("claude: %w: %s", err, strings.TrimSpace(errb.String()))
	}
	return out.String(), nil
}

type generateIn struct {
	Prompt      string `json:"prompt"`
	Title       string `json:"title"`
	Description string `json:"description"`
	BoardID     int64  `json:"boardId"`
}

// POST /api/generate-description → {"description": "<markdown>"}
func (s *server) generateDescription(w http.ResponseWriter, r *http.Request) {
	var in generateIn
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	in.Prompt = strings.TrimSpace(in.Prompt)
	if n := utf8.RuneCountInString(in.Prompt); n < 1 || n > 2000 {
		writeErr(w, store.ValidationError{Msg: "prompt must be 1-2000 characters"})
		return
	}
	if utf8.RuneCountInString(in.Title) > 200 || utf8.RuneCountInString(in.Description) > 10000 {
		writeErr(w, store.ValidationError{Msg: "title or description is too long"})
		return
	}

	var msg strings.Builder
	if in.BoardID != 0 {
		b, err := s.st.GetBoard(in.BoardID)
		if err != nil {
			writeErr(w, err)
			return
		}
		fmt.Fprintf(&msg, "Product: %s\n", b.Name)
		if b.Description != "" {
			fmt.Fprintf(&msg, "About the product: %s\n", b.Description)
		}
	}
	if t := strings.TrimSpace(in.Title); t != "" {
		fmt.Fprintf(&msg, "Ticket title: %s\n", t)
	}
	if d := strings.TrimSpace(in.Description); d != "" {
		fmt.Fprintf(&msg, "\nExisting description:\n%s\n", d)
	}
	fmt.Fprintf(&msg, "\nOwner's idea:\n%s\n", in.Prompt)

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()
	out, err := runClaude(ctx, descriptionSystemPrompt, msg.String())
	out = strings.TrimSpace(out)
	if err != nil || out == "" {
		log.Printf("generate-description failed: err=%v empty=%v", err, out == "")
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "Claude could not generate a description. Try again."})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"description": out})
}
