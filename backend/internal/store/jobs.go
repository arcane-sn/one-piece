package store

import (
	"database/sql"
	"fmt"
)

// Job is one background Generate (Agent Tasks, QA Test Cases, a flow, a description).
// It outlives the HTTP request and the modal that started it.
type Job struct {
	ID       int64  `json:"id"`
	Kind     string `json:"kind"`  // agent_tasks | qa_cases | flow | description
	Scope    string `json:"scope"` // card | item | board
	TargetID int64  `json:"targetId"`
	CardID   int64  `json:"cardId"` // the ticket it belongs to (0 for a New Ticket draft); drives the kanban spinner
	Label    string `json:"label"`  // e.g. "Agent Tasks MID-3", shown in toasts
	Status   string `json:"status"` // running | done | error
	Request  string `json:"request"`
	Result   string `json:"result"` // JSON, kind-specific
	Error    string `json:"error"`
	Seen     bool   `json:"seen"`     // its finish toast was shown
	Consumed bool   `json:"consumed"` // the owner used or dismissed the result
}

const jobCols = `id,kind,scope,target_id,card_id,label,status,request,result,error,seen,consumed`

func scanJob(sc interface{ Scan(...any) error }) (Job, error) {
	var j Job
	err := sc.Scan(&j.ID, &j.Kind, &j.Scope, &j.TargetID, &j.CardID, &j.Label, &j.Status, &j.Request, &j.Result, &j.Error, &j.Seen, &j.Consumed)
	return j, err
}

func (s *Store) queryJobs(q string, args ...any) ([]Job, error) {
	rows, err := s.DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Job{}
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// CreateJob starts a running job; a second one of the same kind on the same target is refused.
func (s *Store) CreateJob(j Job) (Job, error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return Job{}, err
	}
	defer tx.Rollback()
	var n int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM jobs WHERE kind=? AND scope=? AND target_id=? AND status='running'`, j.Kind, j.Scope, j.TargetID).Scan(&n); err != nil {
		return Job{}, err
	}
	if n > 0 {
		return Job{}, ConflictError{Msg: "Already generating. Wait for it to finish."}
	}
	res, err := tx.Exec(`INSERT INTO jobs(kind,scope,target_id,card_id,label,request) VALUES(?,?,?,?,?,?)`,
		j.Kind, j.Scope, j.TargetID, j.CardID, j.Label, j.Request)
	if err != nil {
		return Job{}, err
	}
	id, _ := res.LastInsertId()
	if err := tx.Commit(); err != nil {
		return Job{}, err
	}
	return s.GetJob(id)
}

func (s *Store) GetJob(id int64) (Job, error) {
	j, err := scanJob(s.DB.QueryRow(`SELECT `+jobCols+` FROM jobs WHERE id=?`, id))
	if err == sql.ErrNoRows {
		return Job{}, ErrNotFound
	}
	return j, err
}

// FinishJob records the outcome of a running job. consume marks the result as already used
// (jobs that apply themselves, like Agent Tasks). A job that was deleted meanwhile is ignored.
func (s *Store) FinishJob(id int64, result, errMsg string, consume bool) error {
	status := "done"
	if errMsg != "" {
		status = "error"
	}
	_, err := s.DB.Exec(`UPDATE jobs SET status=?, result=?, error=?, consumed=? WHERE id=? AND status='running'`, status, result, errMsg, consume, id)
	return err
}

// LatestJobs returns the newest job of each kind for one target.
func (s *Store) LatestJobs(scope string, target int64) ([]Job, error) {
	return s.queryJobs(`SELECT `+jobCols+` FROM jobs WHERE id IN (
		SELECT MAX(id) FROM jobs WHERE scope=? AND target_id=? GROUP BY kind) ORDER BY id`, scope, target)
}

// ActiveJobs returns jobs that are running, plus finished ones whose toast has not been shown.
func (s *Store) ActiveJobs() ([]Job, error) {
	return s.queryJobs(`SELECT ` + jobCols + ` FROM jobs WHERE status='running' OR seen=0 ORDER BY id`)
}

func (s *Store) MarkJob(id int64, field string) error {
	if field != "seen" && field != "consumed" {
		return fmt.Errorf("bad job field %q", field)
	}
	res, err := s.DB.Exec(`UPDATE jobs SET `+field+`=1 WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// FailRunningJobs ends jobs a previous shutdown cut short, and drops old finished ones.
func (s *Store) FailRunningJobs() error {
	if _, err := s.DB.Exec(`UPDATE jobs SET status='error', error='Server restarted before this finished.' WHERE status='running'`); err != nil {
		return err
	}
	_, err := s.DB.Exec(`DELETE FROM jobs WHERE status!='running' AND seen=1 AND id NOT IN (SELECT MAX(id) FROM jobs GROUP BY kind,scope,target_id)`)
	return err
}

// DeleteJobsFor removes the jobs of a deleted card, item or board.
func (s *Store) DeleteJobsFor(scope string, target int64) {
	s.DB.Exec(`DELETE FROM jobs WHERE scope=? AND target_id=?`, scope, target)
}
