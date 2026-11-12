package store

import "testing"

func TestJobsLifecycle(t *testing.T) {
	s, c := newCard(t)
	j, err := s.CreateJob(Job{Kind: "agent_tasks", Scope: "card", TargetID: c.ID, CardID: c.ID, Label: "Agent Tasks X"})
	if err != nil || j.Status != "running" {
		t.Fatalf("create = %+v %v", j, err)
	}
	if _, err := s.CreateJob(Job{Kind: "agent_tasks", Scope: "card", TargetID: c.ID}); err == nil {
		t.Fatal("a second running job of the same kind on the same target must be refused")
	}
	if _, err := s.CreateJob(Job{Kind: "qa_cases", Scope: "card", TargetID: c.ID}); err != nil {
		t.Fatalf("another kind is fine: %v", err)
	}
	if act, _ := s.ActiveJobs(); len(act) != 2 {
		t.Fatalf("active = %+v", act)
	}
	s.FinishJob(j.ID, `{"ok":1}`, "", false)
	got, _ := s.GetJob(j.ID)
	if got.Status != "done" || got.Result != `{"ok":1}` || got.Consumed {
		t.Fatalf("finished = %+v", got)
	}
	s.FinishJob(j.ID, "", "late", false) // already finished: ignored
	if got, _ = s.GetJob(j.ID); got.Status != "done" {
		t.Fatalf("finish twice = %+v", got)
	}
	if err := s.MarkJob(j.ID, "seen"); err != nil {
		t.Fatal(err)
	}
	if act, _ := s.ActiveJobs(); len(act) != 1 || act[0].Kind != "qa_cases" {
		t.Fatalf("seen job should leave the active list: %+v", act)
	}
	if again, err := s.CreateJob(Job{Kind: "agent_tasks", Scope: "card", TargetID: c.ID}); err != nil || again.ID == j.ID {
		t.Fatalf("a new job after the old one finished: %+v %v", again, err)
	}
	latest, _ := s.LatestJobs("card", c.ID)
	if len(latest) != 2 {
		t.Fatalf("latest per kind = %+v", latest)
	}
	if err := s.FailRunningJobs(); err != nil {
		t.Fatal(err)
	}
	if act, _ := s.ActiveJobs(); len(act) != 2 || act[0].Status != "error" {
		t.Fatalf("after restart sweep: %+v", act)
	}
	s.DeleteJobsFor("card", c.ID)
	if latest, _ = s.LatestJobs("card", c.ID); len(latest) != 0 {
		t.Fatalf("delete = %+v", latest)
	}
}
