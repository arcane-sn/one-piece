package store

import (
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/arcane-sn/one-piece/backend/internal/qa"
)

// GetQAConfig returns the board's QA stack ("not configured" is an empty Config).
func (s *Store) GetQAConfig(boardID int64) (qa.Config, error) {
	c := qa.Config{AllowedHosts: []string{}, Services: []qa.Service{}}
	var hosts string
	var headed int
	err := s.DB.QueryRow(`SELECT base_url,login_url,session_check_url,allowed_hosts,headed FROM board_qa WHERE board_id=?`, boardID).
		Scan(&c.BaseURL, &c.LoginURL, &c.SessionCheckURL, &hosts, &headed)
	if errors.Is(err, sql.ErrNoRows) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	json.Unmarshal([]byte(hosts), &c.AllowedHosts)
	c.Headed = headed == 1
	rows, err := s.DB.Query(`SELECT id,name,folder,command,port,ready_url,ready_timeout_s FROM board_qa_services WHERE board_id=? ORDER BY position`, boardID)
	if err != nil {
		return c, err
	}
	defer rows.Close()
	for rows.Next() {
		var v qa.Service
		if err := rows.Scan(&v.ID, &v.Name, &v.Folder, &v.Command, &v.Port, &v.ReadyURL, &v.ReadyTimeoutS); err != nil {
			return c, err
		}
		c.Services = append(c.Services, v)
	}
	return c, rows.Err()
}

// SetQAConfig validates and replaces the board's QA stack. An empty config (no base URL, no services) removes it.
func (s *Store) SetQAConfig(boardID int64, c qa.Config) (qa.Config, error) {
	b, err := s.GetBoard(boardID)
	if err != nil {
		return c, err
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return c, err
	}
	defer tx.Rollback()
	tx.Exec(`DELETE FROM board_qa WHERE board_id=?`, boardID)
	tx.Exec(`DELETE FROM board_qa_services WHERE board_id=?`, boardID)
	if c.BaseURL != "" || len(c.Services) > 0 {
		if err := c.Validate(b.Folders); err != nil {
			return c, ValidationError{err.Error()}
		}
		hosts, _ := json.Marshal(c.AllowedHosts)
		h := 0
		if c.Headed {
			h = 1
		}
		if _, err := tx.Exec(`INSERT INTO board_qa(board_id,base_url,login_url,session_check_url,allowed_hosts,headed) VALUES(?,?,?,?,?,?)`,
			boardID, c.BaseURL, c.LoginURL, c.SessionCheckURL, string(hosts), h); err != nil {
			return c, err
		}
		for i, v := range c.Services {
			if _, err := tx.Exec(`INSERT INTO board_qa_services(board_id,position,name,folder,command,port,ready_url,ready_timeout_s) VALUES(?,?,?,?,?,?,?,?)`,
				boardID, i, v.Name, v.Folder, v.Command, v.Port, v.ReadyURL, v.ReadyTimeoutS); err != nil {
				return c, err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return c, err
	}
	return s.GetQAConfig(boardID)
}

// QAStep is one step of one attempt.
type QAStep struct {
	Attempt    int    `json:"attempt"`
	Position   int    `json:"position"`
	Title      string `json:"title"`
	Status     string `json:"status"`
	Screenshot string `json:"screenshot"`
	DurationMs int    `json:"durationMs"`
	Error      string `json:"error"`
}

// QARun is one browser QA run of a QA Test Case.
type QARun struct {
	ID           int64          `json:"id"`
	ItemID       int64          `json:"itemId"`
	Status       string         `json:"status"` // running | passed | failed | blocked | stopped
	BlockReason  string         `json:"blockReason"`
	VerdictIssue string         `json:"verdictIssue"`
	Summary      string         `json:"summary"`
	Attempts     int            `json:"attempts"`
	Repos        []qa.RepoState `json:"repos"`
	BlockedHosts []string       `json:"blockedHosts"`
	StartedAt    string         `json:"startedAt"`
	FinishedAt   string         `json:"finishedAt"`
	Steps        []QAStep       `json:"steps"`
}

const qaKeep = 10

func (s *Store) CreateQARun(itemID, cardID, boardID int64, repos []qa.RepoState) (int64, error) {
	b, _ := json.Marshal(repos)
	res, err := s.DB.Exec(`INSERT INTO qa_runs(item_id,card_id,board_id,repos,content_version) VALUES(?,?,?,?,(SELECT content_version FROM cards WHERE id=?))`, itemID, cardID, boardID, string(b), cardID)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// FinishQARun settles a run.
func (s *Store) FinishQARun(id int64, status, blockReason, issue, summary string, attempts int, blocked []string) error {
	b, _ := json.Marshal(blocked)
	_, err := s.DB.Exec(`UPDATE qa_runs SET status=?,block_reason=?,verdict_issue=?,summary=?,attempts=?,blocked_hosts=?,
		finished_at=strftime('%Y-%m-%dT%H:%M:%SZ','now') WHERE id=?`, status, blockReason, issue, summary, attempts, string(b), id)
	return err
}

func (s *Store) AddQASteps(runID int64, attempt int, steps []qa.Step) error {
	for _, st := range steps {
		if _, err := s.DB.Exec(`INSERT INTO qa_steps(run_id,attempt,position,title,status,screenshot,duration_ms,error) VALUES(?,?,?,?,?,?,?,?)`,
			runID, attempt, st.Position, st.Title, st.Status, st.Screenshot, st.DurationMs, st.Error); err != nil {
			return err
		}
	}
	return nil
}

// GetQARun returns one run with its steps.
func (s *Store) GetQARun(id int64) (QARun, error) {
	runs, err := s.listQARuns(`WHERE id=?`, id)
	if err != nil || len(runs) == 0 {
		if err == nil {
			err = ErrNotFound
		}
		return QARun{}, err
	}
	return runs[0], nil
}

// ListQARuns returns an item's runs, newest first, with steps.
func (s *Store) ListQARuns(itemID int64) ([]QARun, error) {
	return s.listQARuns(`WHERE item_id=?`, itemID)
}

func (s *Store) listQARuns(where string, arg int64) ([]QARun, error) {
	rows, err := s.DB.Query(`SELECT id,item_id,status,block_reason,verdict_issue,summary,attempts,repos,blocked_hosts,started_at,finished_at FROM qa_runs `+where+` ORDER BY id DESC`, arg)
	if err != nil {
		return nil, err
	}
	out := []QARun{}
	for rows.Next() {
		var r QARun
		var repos, hosts string
		if err := rows.Scan(&r.ID, &r.ItemID, &r.Status, &r.BlockReason, &r.VerdictIssue, &r.Summary, &r.Attempts, &repos, &hosts, &r.StartedAt, &r.FinishedAt); err != nil {
			rows.Close()
			return nil, err
		}
		r.Repos, r.BlockedHosts, r.Steps = []qa.RepoState{}, []string{}, []QAStep{}
		json.Unmarshal([]byte(repos), &r.Repos)
		json.Unmarshal([]byte(hosts), &r.BlockedHosts)
		out = append(out, r)
	}
	rows.Close()
	for i := range out {
		sr, err := s.DB.Query(`SELECT attempt,position,title,status,screenshot,duration_ms,error FROM qa_steps WHERE run_id=? ORDER BY attempt,position`, out[i].ID)
		if err != nil {
			return nil, err
		}
		for sr.Next() {
			var x QAStep
			if err := sr.Scan(&x.Attempt, &x.Position, &x.Title, &x.Status, &x.Screenshot, &x.DurationMs, &x.Error); err != nil {
				sr.Close()
				return nil, err
			}
			out[i].Steps = append(out[i].Steps, x)
		}
		sr.Close()
	}
	return out, nil
}

// PruneQARuns keeps the newest 10 runs of an item and returns the ids it deleted (the caller removes their folders).
func (s *Store) PruneQARuns(itemID int64) ([]int64, error) {
	rows, err := s.DB.Query(`SELECT id FROM qa_runs WHERE item_id=? ORDER BY id DESC LIMIT -1 OFFSET ?`, itemID, qaKeep)
	if err != nil {
		return nil, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		rows.Scan(&id)
		ids = append(ids, id)
	}
	rows.Close()
	for _, id := range ids {
		s.DB.Exec(`DELETE FROM qa_runs WHERE id=?`, id)
	}
	return ids, nil
}

// QARunIDs lists every run id (to sweep folders left by deleted items or cards).
func (s *Store) QARunIDs() (map[int64]bool, error) {
	rows, err := s.DB.Query(`SELECT id FROM qa_runs`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := map[int64]bool{}
	for rows.Next() {
		var id int64
		rows.Scan(&id)
		m[id] = true
	}
	return m, rows.Err()
}

// StopRunningQARuns marks runs left "running" by a previous server process as stopped.
func (s *Store) StopRunningQARuns() error {
	_, err := s.DB.Exec(`UPDATE qa_runs SET status='stopped', finished_at=strftime('%Y-%m-%dT%H:%M:%SZ','now') WHERE status='running'`)
	return err
}

// LastQAStatus returns the status of an item's newest run ("" when it has none).
func (s *Store) LastQAStatus(itemID int64) string {
	var st string
	s.DB.QueryRow(`SELECT status FROM qa_runs WHERE item_id=? ORDER BY id DESC LIMIT 1`, itemID).Scan(&st)
	return st
}

// SetQARunRepos records the repos under test (branch, commit, dirty) once they are known.
func (s *Store) SetQARunRepos(id int64, repos []qa.RepoState) error {
	b, _ := json.Marshal(repos)
	_, err := s.DB.Exec(`UPDATE qa_runs SET repos=? WHERE id=?`, string(b), id)
	return err
}
