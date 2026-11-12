package store

import "testing"

func TestDiscussMessagesAndState(t *testing.T) {
	s, c := newCard(t)
	if _, err := s.AddDiscussMessage(c.ID, "user", "which repos?", "done"); err != nil {
		t.Fatal(err)
	}
	m, _ := s.AddDiscussMessage(c.ID, "assistant", "", "running")
	s.AppendDiscussText(m.ID, "first")
	s.AppendDiscussText(m.ID, "second")
	if running, _ := s.RunningDiscussTurn(c.ID); !running {
		t.Fatal("a running assistant message means a running turn")
	}
	s.FinishDiscussMessage(m.ID, "done")
	if running, _ := s.RunningDiscussTurn(c.ID); running {
		t.Fatal("turn should be finished")
	}
	ms, _ := s.ListDiscussMessages(c.ID)
	if len(ms) != 2 || ms[0].Role != "user" || ms[1].Text != "first\n\nsecond" || ms[1].Status != "done" {
		t.Fatalf("messages = %+v", ms)
	}

	if st, _ := s.GetDiscussState(c.ID); st != "" {
		t.Fatalf("fresh state = %q", st)
	}
	s.SetDiscussState(c.ID, "sess-1")
	s.SetDiscussState(c.ID, "sess-2")
	if st, _ := s.GetDiscussState(c.ID); st != "sess-2" {
		t.Fatalf("state = %q", st)
	}

	r, _ := s.AddDiscussMessage(c.ID, "assistant", "half", "running")
	if err := s.FailRunningDiscuss(); err != nil {
		t.Fatal(err)
	}
	ms, _ = s.ListDiscussMessages(c.ID)
	if got := ms[len(ms)-1]; got.ID != r.ID || got.Status != "stopped" {
		t.Fatalf("left-over running message = %+v", got)
	}

	other, _ := s.CreateCard(c.BoardID, "Other", "", "")
	s.AddDiscussMessage(other.ID, "user", "x", "done")
	if err := s.DeleteCard(c.ID); err != nil {
		t.Fatal(err)
	}
	if ms, _ := s.ListDiscussMessages(c.ID); len(ms) != 0 {
		t.Errorf("cascade left %d messages", len(ms))
	}
	if st, _ := s.GetDiscussState(c.ID); st != "" {
		t.Errorf("cascade left state %q", st)
	}
	if ms, _ := s.ListDiscussMessages(other.ID); len(ms) != 1 {
		t.Errorf("other card lost its messages")
	}
}
