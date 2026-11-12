package store

import "testing"

func TestReviseCardKeepsRevisionAndRestores(t *testing.T) {
	s, c := newCard(t)
	s.DB.Exec(`UPDATE cards SET status='business', approved=1 WHERE id=?`, c.ID)
	v1, _ := s.GetCard(c.ID)

	if _, rev, err := s.ReviseCard(c.ID, v1.Title, v1.Description); err != nil || rev != 0 {
		t.Fatalf("same text must be a no-op: rev=%d err=%v", rev, err)
	}
	next, rev, err := s.ReviseCard(c.ID, "New title", "New body")
	if err != nil || rev == 0 || next.ContentVersion != v1.ContentVersion+1 || next.Approved {
		t.Fatalf("revise = %+v rev=%d err=%v", next, rev, err)
	}
	if r, _ := s.GetRevision(rev); r.Title != v1.Title || r.Description != v1.Description || r.Version != v1.ContentVersion {
		t.Fatalf("revision = %+v", r)
	}
	// a verdict is recorded for the text as it is now; a later edit outdates it
	vd, err := s.RecordVerdict(c.ID, "pass", "ok", nil)
	if err != nil || vd.ContentVersion != next.ContentVersion {
		t.Fatalf("verdict = %+v %v", vd, err)
	}
	if _, err := s.SetApproved(c.ID, true); err != nil {
		t.Fatalf("a passing verdict of the current text allows approval: %v", err)
	}
	back, _, err := s.RestoreRevision(c.ID, rev)
	if err != nil || back.Title != v1.Title || back.Description != v1.Description || back.Approved || back.ContentVersion != next.ContentVersion+1 {
		t.Fatalf("restore = %+v %v", back, err)
	}
	if _, err := s.SetApproved(c.ID, true); err == nil {
		t.Fatal("the verdict is outdated after a restore")
	}
	if _, _, err := s.RestoreRevision(c.ID+99, rev); err != ErrNotFound {
		t.Fatalf("revision of another card: %v", err)
	}
	if _, err := s.RecordVerdict(c.ID, "maybe", "", nil); err == nil {
		t.Fatal("bad verdict must be refused")
	}
}

func TestRevisionsArePruned(t *testing.T) {
	s, c := newCard(t)
	for i := 0; i < keepRevisions+5; i++ {
		s.ReviseCard(c.ID, "T", "body "+string(rune('a'+i)))
	}
	var n int
	s.DB.QueryRow(`SELECT COUNT(*) FROM card_revisions WHERE card_id=?`, c.ID).Scan(&n)
	if n != keepRevisions {
		t.Fatalf("revisions kept = %d", n)
	}
}

func TestReviewMessagesAndSwimlaneCheck(t *testing.T) {
	s, c := newCard(t)
	m, _ := s.AddReviewMessage(c.ID, "assistant", "", "running", 0)
	s.AppendReviewText(m.ID, "a")
	s.AppendReviewText(m.ID, "b")
	s.AddReviewMessage(c.ID, "event", "Description updated", "done", 7)
	got, _ := s.ListReviewMessages(c.ID)
	if len(got) != 2 || got[0].Text != "a\n\nb" || got[1].Ref != 7 {
		t.Fatalf("messages = %+v", got)
	}
	s.FailRunningReviewChats()
	if got, _ = s.ListReviewMessages(c.ID); got[0].Status != "stopped" {
		t.Fatalf("swept = %+v", got)
	}
	if err := CheckSwimlane("title: x\nA -> B: hi"); err != nil {
		t.Fatal(err)
	}
	if err := CheckSwimlane("just words"); err == nil {
		t.Fatal("prose is not a flow")
	}
}
