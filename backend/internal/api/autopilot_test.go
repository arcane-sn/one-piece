package api

import (
	"context"
	"testing"

	"github.com/arcane-sn/one-piece/backend/internal/store"
)

func TestQAAutopilotFixesRetestsAndFinishes(t *testing.T) {
	s, _, _, card, _ := dFixture(t)
	s.St.DB.Exec(`UPDATE cards SET status='qa', approved=1 WHERE id=?`, card.ID)
	qa, _ := s.St.CreateItem(card.ID, "qa", "Login works", "steps")
	fix, _ := s.St.CreateItem(card.ID, "agent", "[FIX] Login works", "fix it")
	s.St.SetItemStatus(qa.ID, "todo")
	s.St.SetItemStatus(fix.ID, "todo")

	var order []string
	runList := func(list []store.Item) bool {
		for _, it := range list {
			order = append(order, it.Kind)
			s.St.SetItemStatus(it.ID, "done") // the fix lands, then the retest passes
		}
		return true
	}
	(&server{st: s.St}).qaAutopilot(context.Background(), card.ID, runList)

	if len(order) != 2 || order[0] != "agent" || order[1] != "qa" {
		t.Fatalf("order = %v, want the fix first, then the retest", order)
	}
	if c, _ := s.St.GetCard(card.ID); c.Status != "done" {
		t.Fatalf("ticket status = %s, want done", c.Status)
	}
}

func TestQAAutopilotStopsWithoutFixes(t *testing.T) {
	s, _, _, card, _ := dFixture(t)
	s.St.DB.Exec(`UPDATE cards SET status='qa', approved=1 WHERE id=?`, card.ID)
	qa, _ := s.St.CreateItem(card.ID, "qa", "Login works", "steps")
	s.St.SetItemStatus(qa.ID, "todo")
	(&server{st: s.St}).qaAutopilot(context.Background(), card.ID, func([]store.Item) bool { t.Fatal("nothing to run"); return true })
	if c, _ := s.St.GetCard(card.ID); c.Status != "qa" {
		t.Fatalf("status = %s, must stay in QA", c.Status)
	}
}
