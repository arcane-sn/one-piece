package api

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"
)

func liveCount(t *testing.T, s *testSrv, cardID int64, kind string) int {
	t.Helper()
	items, _ := s.St.ListItems(cardID)
	n := 0
	for _, it := range items {
		if it.Kind == kind && !it.Superseded {
			n++
		}
	}
	return n
}

func TestMovingToBusinessGeneratesTasksThenCases(t *testing.T) {
	useStub(t, passing, nil)
	s := newTestServer(t)
	ck := s.login(t)
	bp := newBoardPath(t, s, ck)
	bid, _ := strconv.ParseInt(bp[strings.LastIndex(bp, "/")+1:], 10, 64)
	defer func(f func(context.Context, string, string) (string, error)) { runClaude = f }(runClaude)
	runClaude = func(context.Context, string, string) (string, error) { return itemsReply, nil }
	d, err := s.St.CreateDraft(bid, "Login", "Let the owner sign in")
	if err != nil {
		t.Fatal(err)
	}
	cp := "/api/cards/" + strconv.FormatInt(d.ID, 10)
	if n := liveCount(t, s, d.ID, "agent"); n != 0 {
		t.Fatalf("a Draft has no plan yet: %d", n)
	}
	if resp, body := s.req(t, "POST", cp+"/move", map[string]any{"status": "business", "position": 0}, ck); resp.StatusCode != 200 {
		t.Fatalf("move: %d %s", resp.StatusCode, body)
	}
	waitFor(t, "tasks and QA cases generated", func() bool {
		return liveCount(t, s, d.ID, "agent") == 2 && liveCount(t, s, d.ID, "qa") == 1
	})
	if c, _ := s.St.GetCard(d.ID); c.Approved {
		t.Fatal("generating must never approve")
	}
	// a second visit to Business keeps the owner's plan: nothing is generated again
	s.req(t, "POST", cp+"/move", map[string]any{"status": "draft", "position": 0}, ck)
	s.req(t, "POST", cp+"/move", map[string]any{"status": "business", "position": 0}, ck)
	time.Sleep(300 * time.Millisecond)
	if a, q := liveCount(t, s, d.ID, "agent"), liveCount(t, s, d.ID, "qa"); a != 2 || q != 1 {
		t.Fatalf("regenerated on a second visit: %d tasks, %d cases", a, q)
	}
}

func TestGenerationFinishingAfterTheMoveWritesNothing(t *testing.T) {
	s, ck, cp, card, _, _ := contractFixture(t)
	before, _ := s.St.ListItems(card.ID)
	release, started := make(chan struct{}), make(chan struct{})
	defer func(f func(context.Context, string, string) (string, error)) { runClaude = f }(runClaude)
	runClaude = func(context.Context, string, string) (string, error) {
		close(started)
		<-release
		return itemsReply, nil
	}
	resp, _ := s.req(t, "POST", cp+"/generate-items", map[string]any{"kind": "agent"}, ck)
	if resp.StatusCode != 202 {
		t.Fatalf("start: %d", resp.StatusCode)
	}
	<-started
	if resp, body := s.req(t, "POST", cp+"/move", map[string]any{"status": "agent", "position": 0}, ck); resp.StatusCode != 200 {
		t.Fatalf("move: %d %s", resp.StatusCode, body)
	}
	close(release)
	waitFor(t, "the job to finish", func() bool {
		js, _ := s.St.LatestJobs("card", card.ID)
		return len(js) > 0 && js[0].Status != "running"
	})
	after, _ := s.St.ListItems(card.ID)
	if len(after) != len(before) {
		t.Fatalf("an approved ticket in Agent got new items: %d -> %d", len(before), len(after))
	}
	if c, _ := s.St.GetCard(card.ID); !c.Approved {
		t.Fatal("approval must survive a late generation")
	}
}

func TestGenerationOutsideBusinessIsRefusedForDrafts(t *testing.T) {
	useStub(t, passing, nil)
	s := newTestServer(t)
	ck := s.login(t)
	bp := newBoardPath(t, s, ck)
	bid, _ := strconv.ParseInt(bp[strings.LastIndex(bp, "/")+1:], 10, 64)
	d, _ := s.St.CreateDraft(bid, "Idea", "rough")
	cp := "/api/cards/" + strconv.FormatInt(d.ID, 10)
	if resp, _ := s.req(t, "POST", cp+"/generate-items", map[string]any{}, ck); resp.StatusCode != 409 {
		t.Fatalf("generate on a Draft: %d, want 409", resp.StatusCode)
	}
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for i := 0; i < 500; i++ {
		if ok() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}
