package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"

	"github.com/arcane-sn/one-piece/backend/internal/store"
)

// autopilotRounds is how many times a QA run may send failures back through [FIX] tasks before it stops and
// leaves the ticket in QA for the owner.
const autopilotRounds = 3

// autoGenerateQA drafts the QA Test Cases of a ticket that just moved to QA (the owner reviews them, then runs
// them). It does nothing when the ticket already has cases, so a second Run All never replaces the owner's review.
func (s *server) autoGenerateQA(cardID int64) {
	items, err := s.st.ListItems(cardID)
	if err != nil {
		return
	}
	for _, it := range items {
		if it.Kind == "qa" && !it.Superseded {
			return
		}
	}
	r := httptest.NewRequest("POST", "/api/cards/"+strconv.FormatInt(cardID, 10)+"/generate-items", strings.NewReader(`{"kind":"qa"}`))
	r.SetPathValue("id", strconv.FormatInt(cardID, 10))
	rec := httptest.NewRecorder()
	s.generateItems(rec, r)
	if rec.Code != http.StatusAccepted {
		s.logCard(cardID, "Could not start QA generation: "+strings.TrimSpace(rec.Body.String()))
	}
}

// qaAutopilot runs after a QA Run All: while a test case is still not done and [FIX] tasks exist, it runs the
// fixes, then runs the failed test cases again. When every QA case is done the ticket moves to Done.
func (s *server) qaAutopilot(ctx context.Context, cardID int64, runList func([]store.Item) bool) {
	pick := func(kind, status string) (todo []store.Item, all []store.Item) {
		items, _ := s.st.ListItems(cardID)
		for _, it := range items {
			if it.Kind == kind && !it.Superseded {
				all = append(all, it)
				if it.Status == status {
					todo = append(todo, it)
				}
			}
		}
		return
	}
	for round := 0; ; round++ {
		if ctx.Err() != nil {
			return
		}
		qaTodo, qaAll := pick("qa", "todo")
		if len(qaAll) > 0 && len(qaTodo) == 0 {
			s.finishTicket(cardID)
			return
		}
		fixes, _ := pick("agent", "todo")
		if len(fixes) == 0 || round >= autopilotRounds {
			s.logCard(cardID, "Autopilot stopped: "+strconv.Itoa(len(qaTodo))+" QA case(s) still failing; the ticket stays in QA for you.")
			return
		}
		if !runList(fixes) { // a fix that fails or is blocked needs the owner
			s.logCard(cardID, "Autopilot stopped: a [FIX] task did not finish.")
			return
		}
		again, _ := pick("qa", "todo")
		if !runList(again) {
			return
		}
	}
}

// finishTicket moves a QA ticket whose cases all passed to Done.
func (s *server) finishTicket(cardID int64) {
	c, err := s.st.GetCard(cardID)
	if err != nil || c.Status != "qa" {
		return
	}
	if _, err := s.st.MoveCard(cardID, "done", 1<<30); err != nil {
		return
	}
	s.logCard(cardID, "All QA cases passed: ticket moved to Done")
}

// logCard notes something on the ticket's newest QA case (items carry the log; the ticket itself has none).
func (s *server) logCard(cardID int64, text string) {
	items, _ := s.st.ListItems(cardID)
	for i := len(items) - 1; i >= 0; i-- {
		if !items[i].Superseded && items[i].Kind == "qa" {
			s.st.AppendItemLog(items[i].ID, text)
			return
		}
	}
}
