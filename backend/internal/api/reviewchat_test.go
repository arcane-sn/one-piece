package api

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/arcane-sn/one-piece/backend/internal/store"
)

type rcFn = func(ctx context.Context, in reviewRun, onText func(string)) (string, error)

func useReviewChat(t *testing.T, fn rcFn) *[]reviewRun {
	t.Helper()
	var mu sync.Mutex
	var calls []reviewRun
	old := runReviewChat
	runReviewChat = func(ctx context.Context, in reviewRun, onText func(string)) (string, error) {
		mu.Lock()
		calls = append(calls, in)
		mu.Unlock()
		return fn(ctx, in, onText)
	}
	t.Cleanup(func() { runReviewChat = old })
	return &calls
}

func rcGet(t *testing.T, s *testSrv, cp string, ck *http.Cookie) reviewChatView {
	t.Helper()
	resp, body := s.req(t, "GET", cp+"/review-chat", nil, ck)
	if resp.StatusCode != 200 {
		t.Fatalf("get review chat: %d %s", resp.StatusCode, body)
	}
	var v reviewChatView
	json.Unmarshal(body, &v)
	return v
}

func rcIdle(t *testing.T, s *testSrv, cp string, ck *http.Cookie) reviewChatView {
	t.Helper()
	for i := 0; i < 200; i++ {
		if v := rcGet(t, s, cp, ck); !v.Running {
			return v
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatal("the review turn never finished")
	return reviewChatView{}
}

func rcSend(t *testing.T, s *testSrv, cp string, ck *http.Cookie, text string) (int, string) {
	t.Helper()
	resp, body := s.req(t, "POST", cp+"/review-chat/messages", map[string]string{"text": text}, ck)
	return resp.StatusCode, string(body)
}

func TestReviewChatTurnSessionAndJob(t *testing.T) {
	calls := useReviewChat(t, func(_ context.Context, _ reviewRun, onText func(string)) (string, error) {
		onText("Two things are unclear.")
		return "sess-1", nil
	})
	s, ck, cp, card, folder := dFixture(t)
	if code, body := rcSend(t, s, cp, ck, "please review"); code != 202 {
		t.Fatalf("send: %d %s", code, body)
	}
	v := rcIdle(t, s, cp, ck)
	if !v.Available || len(v.Messages) != 2 || v.Messages[1].Text != "Two things are unclear." || v.Messages[1].Status != "done" {
		t.Fatalf("view = %+v", v)
	}
	if sid, _ := s.St.GetReviewState(card.ID); sid != "sess-1" {
		t.Fatalf("session = %q", sid)
	}
	in := (*calls)[0]
	if in.CardID != card.ID || in.DB != s.St.Path || in.SessionID != "" || len(in.Dirs) != 1 || in.Dirs[0] != folder ||
		!strings.Contains(in.Prompt, card.Ident) || !strings.Contains(in.Prompt, "please review") || !strings.Contains(in.Prompt, "review-verdict") {
		t.Fatalf("call = %+v", in)
	}
	for _, want := range []string{"ticket-edit", "flow-add", "review-verdict", "never approve", "Business Flow"} {
		if !strings.Contains(in.System, want) {
			t.Errorf("system prompt lacks %q", want)
		}
	}
	// the turn is a job: the owner gets a toast and the card a spinner, and it is not a result to "use"
	js, _ := s.St.LatestJobs("card", card.ID)
	if len(js) != 1 || js[0].Kind != "review_chat" || js[0].Status != "done" || !js[0].Consumed || js[0].Label != "Review "+card.Ident {
		t.Fatalf("jobs = %+v", js)
	}
	// a resumed turn passes the session id; a new session forgets it and leaves a divider
	rcSend(t, s, cp, ck, "again")
	rcIdle(t, s, cp, ck)
	if got := (*calls)[1].SessionID; got != "sess-1" {
		t.Errorf("resume id = %q", got)
	}
	if resp, _ := s.req(t, "POST", cp+"/review-chat/new", map[string]any{}, ck); resp.StatusCode != 204 {
		t.Fatalf("new: %d", resp.StatusCode)
	}
	if sid, _ := s.St.GetReviewState(card.ID); sid != "" {
		t.Fatalf("session after new = %q", sid)
	}
	if v = rcGet(t, s, cp, ck); v.Messages[len(v.Messages)-1].Role != "marker" {
		t.Fatalf("no divider: %+v", v.Messages)
	}
}

func TestReviewChatOnlyInBusinessAndOneTurnAtATime(t *testing.T) {
	release := make(chan struct{})
	useReviewChat(t, func(ctx context.Context, _ reviewRun, onText func(string)) (string, error) {
		onText("working")
		select {
		case <-release:
			return "s", nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	})
	s, ck, cp, card, _ := dFixture(t)
	if code, _ := rcSend(t, s, cp, ck, "go"); code != 202 {
		t.Fatalf("send: %d", code)
	}
	if code, body := rcSend(t, s, cp, ck, "again"); code != 409 || !strings.Contains(body, "still answering") {
		t.Fatalf("second turn while running: %d %s", code, body)
	}
	if v := rcGet(t, s, cp, ck); !v.Running {
		t.Fatal("running not reported")
	}
	// the owner can stop it; a stop is not a failure and raises no toast
	s.req(t, "POST", cp+"/review-chat/stop", map[string]any{}, ck)
	v := rcIdle(t, s, cp, ck)
	if v.Messages[1].Status != "stopped" {
		t.Fatalf("stopped message = %+v", v.Messages[1])
	}
	if act, _ := s.St.ActiveJobs(); len(act) != 0 {
		t.Fatalf("a stopped turn must not leave a toast behind: %+v", act)
	}
	close(release)

	s.req(t, "POST", cp+"/move", map[string]any{"status": "agent", "position": 0}, ck)
	if code, body := rcSend(t, s, cp, ck, "x"); code != 409 || !strings.Contains(body, "Business") {
		t.Fatalf("agent column: %d %s", code, body)
	}
	if v := rcGet(t, s, cp, ck); v.Available {
		t.Error("available outside Business")
	}
	if resp, _ := s.req(t, "GET", "/api/cards/9999/review-chat", nil, ck); resp.StatusCode != 404 {
		t.Fatalf("missing card: %d", resp.StatusCode)
	}
	_ = card
}

func TestReviewChatFailureAndPastedImages(t *testing.T) {
	calls := useReviewChat(t, func(context.Context, reviewRun, func(string)) (string, error) {
		return "", context.DeadlineExceeded
	})
	s, ck, cp, card, _ := dFixture(t)
	_, body := s.req(t, "POST", "/api/uploads", map[string]string{"data": "data:image/png;base64," + tinyPNG}, ck)
	var up struct{ URL string }
	json.Unmarshal(body, &up)
	rcSend(t, s, cp, ck, "see ![]("+up.URL+")")
	v := rcIdle(t, s, cp, ck)
	if v.Messages[1].Status != "failed" || v.Messages[1].Text == "" {
		t.Fatalf("failed turn = %+v", v.Messages)
	}
	in := (*calls)[0]
	if !strings.HasSuffix(in.Dirs[len(in.Dirs)-1], "uploads") || !strings.Contains(in.Prompt, up.URL[len("/api/uploads/"):]) {
		t.Fatalf("pasted image must reach Claude: dirs=%v\n%s", in.Dirs, in.Prompt)
	}
	js, _ := s.St.LatestJobs("card", card.ID)
	if len(js) != 1 || js[0].Status != "error" || js[0].Error == "" {
		t.Fatalf("failed turn must toast: %+v", js)
	}
}

func TestRevisionUndoAndDeletion(t *testing.T) {
	release := make(chan struct{})
	useReviewChat(t, func(ctx context.Context, _ reviewRun, _ func(string)) (string, error) {
		<-release
		return "s", nil
	})
	s, ck, cp, card, _ := dFixture(t)
	_, rev, err := s.St.ReviseCard(card.ID, "Login v2", "Better body")
	if err != nil || rev == 0 {
		t.Fatal(err)
	}
	rp := cp + "/revisions/" + strconv.FormatInt(rev, 10) + "/restore"

	rcSend(t, s, cp, ck, "go")
	if resp, _ := s.req(t, "POST", rp, map[string]any{}, ck); resp.StatusCode != 409 {
		t.Fatalf("undo while Claude works: %d, want 409", resp.StatusCode)
	}
	close(release)
	rcIdle(t, s, cp, ck)

	resp, body := s.req(t, "POST", rp, map[string]any{}, ck)
	var back store.Card
	json.Unmarshal(body, &back)
	if resp.StatusCode != 200 || back.Title != card.Title || back.Description != card.Description {
		t.Fatalf("undo: %d %s", resp.StatusCode, body)
	}
	msgs, _ := s.St.ListReviewMessages(card.ID)
	if last := msgs[len(msgs)-1]; last.Role != "event" || last.Ref == 0 {
		t.Fatalf("undo leaves an event that can itself be undone: %+v", msgs)
	}
	if resp, _ := s.req(t, "POST", cp+"/revisions/9999/restore", map[string]any{}, ck); resp.StatusCode != 404 {
		t.Fatalf("unknown revision: %d", resp.StatusCode)
	}

	// deleting the ticket removes its chat, revisions and jobs
	s.req(t, "DELETE", cp, nil, ck)
	var n int
	s.St.DB.QueryRow(`SELECT (SELECT COUNT(*) FROM review_messages)+(SELECT COUNT(*) FROM card_revisions)+(SELECT COUNT(*) FROM jobs WHERE card_id=?)`, card.ID).Scan(&n)
	if n != 0 {
		t.Fatalf("leftovers after delete: %d", n)
	}
}

func TestRealRunReviewChatToolsAndEnv(t *testing.T) {
	argsFile, envFile := fakeClaude(t, `cat > /dev/null
echo '{"type":"result","is_error":false,"result":"ok","session_id":"s9"}'
`)
	sid, err := realRunReviewChat(context.Background(), reviewRun{System: "S", Prompt: "p", Exe: "/x/op", DB: "/db", BoardID: 3, CardID: 7}, func(string) {})
	if err != nil || sid != "s9" {
		t.Fatalf("%q %v", sid, err)
	}
	a, _ := os.ReadFile(argsFile)
	for _, want := range []string{"Read", "Bash(/x/op ticket-edit:*)", "Bash(/x/op flow-add:*)", "Bash(/x/op review-verdict:*)"} {
		if !strings.Contains(string(a), want) {
			t.Errorf("tools lack %q:\n%s", want, a)
		}
	}
	for _, banned := range []string{"git:", "gh:", "branch", "Write", "Edit", "draft"} {
		if strings.Contains(string(a), banned) {
			t.Errorf("a review session must not get %q:\n%s", banned, a)
		}
	}
	e, _ := os.ReadFile(envFile)
	for _, want := range []string{"ONE_PIECE_CARD=7", "ONE_PIECE_MODE=review", "ONE_PIECE_BOARD=3"} {
		if !strings.Contains(string(e), want) {
			t.Errorf("env lacks %q: %s", want, e)
		}
	}
}
