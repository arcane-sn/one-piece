package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/arcane-sn/one-piece/backend/internal/store"
)

const reviewSystemPrompt = `You review a Business ticket for a product team before any work starts.
Judge only these things:
1. The title and description are clear: a developer could tell what to build.
2. There is a testable Definition of Done (concrete, checkable outcomes).
3. The scope is small and manageable: one focused change, not a whole application.
4. At least one Business Flow (swimlane diagram, written as text) is attached and it matches the description: it shows who does what, in order. A ticket with no Business Flow is "needs_work".
You may use Read, Grep and Glob on the listed product folders to check how the product works today. Keep it short (a handful of tool calls at most).
Your final message must be ONLY one JSON object and nothing else:
{"verdict":"pass"|"needs_work","summary":"one or two sentences","missing":["each concrete thing that is missing or wrong"]}
Use "pass" only when all four points hold; then "missing" is an empty array. With "needs_work", list at least one item in "missing".
Write in the same language as the ticket.`

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
	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Dir = tmp
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

type verdict struct {
	Verdict string   `json:"verdict"`
	Summary string   `json:"summary"`
	Missing []string `json:"missing"`
}

// parseVerdict extracts the JSON object Claude was asked to return, tolerating code fences or prose around it.
func parseVerdict(text string) (verdict, error) {
	a, b := strings.Index(text, "{"), strings.LastIndex(text, "}")
	if a < 0 || b < a {
		return verdict{}, errors.New("no JSON object in the review")
	}
	var v verdict
	if err := json.Unmarshal([]byte(text[a:b+1]), &v); err != nil {
		return verdict{}, err
	}
	if v.Verdict != "pass" && v.Verdict != "needs_work" {
		return verdict{}, fmt.Errorf("unknown verdict %q", v.Verdict)
	}
	if v.Missing == nil {
		v.Missing = []string{}
	}
	v.Summary = strings.TrimSpace(v.Summary)
	return v, nil
}

// startReview begins a review of the card's current content unless one for this
// version already exists (running or done). A failed review is retried.
func (s *server) startReview(c store.Card) (store.Review, error) {
	if c.Status == "draft" {
		return store.Review{}, store.ConflictError{Msg: "Draft tickets are not reviewed. Move it to Business first."}
	}
	if r, err := s.st.LatestReview(c.ID); err == nil && r.ContentVersion == c.ContentVersion && r.Status != "failed" {
		return r, nil
	}
	r, err := s.st.StartReview(c.ID, c.ContentVersion)
	if err != nil {
		return store.Review{}, err
	}
	go s.runReview(r, c)
	return r, nil
}

func (s *server) runReview(r store.Review, c store.Card) {
	var msg strings.Builder
	var folders []string
	if b, err := s.st.GetBoard(c.BoardID); err == nil {
		folders = b.Folders
		fmt.Fprintf(&msg, "Product: %s\n", b.Name)
		if b.Description != "" {
			fmt.Fprintf(&msg, "About the product: %s\n", b.Description)
		}
		if len(folders) > 0 {
			fmt.Fprintf(&msg, "Product folders you may read:\n- %s\n", strings.Join(folders, "\n- "))
		}
	}
	fmt.Fprintf(&msg, "\nTicket title: %s\n", c.Title)
	if c.Deadline != "" {
		fmt.Fprintf(&msg, "Deadline: %s\n", c.Deadline)
	}
	fmt.Fprintf(&msg, "\nTicket description (Markdown):\n%s\n", c.Description)
	flows, _ := s.st.ListFlows(c.ID)
	if len(flows) == 0 {
		msg.WriteString("\nBusiness Flows: none attached.\n")
	}
	for _, f := range flows {
		fmt.Fprintf(&msg, "\nBusiness Flow \"%s\" (swimlane text):\n%s\n", f.Title, f.Source)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	logLine := func(line string) { s.st.AppendReviewLog(r.ID, line) }
	logLine("Review started")
	switch len(folders) {
	case 0:
	case 1:
		logLine("Checking 1 product folder")
	default:
		logLine(fmt.Sprintf("Checking %d product folders", len(folders)))
	}
	final, err := streamClaude(ctx, reviewSystemPrompt, msg.String(), folders, logLine)
	if err != nil {
		log.Printf("review %d failed: %v", r.ID, err)
		s.st.FailReview(r.ID, "Claude could not review this ticket. Try again.")
		return
	}
	v, err := parseVerdict(final)
	if err != nil {
		log.Printf("review %d unreadable: %v", r.ID, err)
		s.st.FailReview(r.ID, "Claude returned a review that could not be read. Try again.")
		return
	}
	logLine("Verdict: " + strings.ReplaceAll(v.Verdict, "_", " "))
	s.st.FinishReview(r.ID, v.Verdict, v.Summary, v.Missing)
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

// POST /api/cards/{id}/review → (re)start a review of the current content.
func (s *server) postReview(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	c, err := s.st.GetCard(id)
	if err != nil {
		writeErr(w, err)
		return
	}
	rv, err := s.startReview(c)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, rv)
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
