package store

import (
	"strconv"
	"strings"
)

// PickedSHAs returns the full source SHAs already cherry-picked for one repo of a ticket.
func (s *Store) PickedSHAs(cardID int64, repo string) ([]string, error) {
	rows, err := s.DB.Query(`SELECT source_sha FROM card_picks WHERE card_id=? AND repo=? ORDER BY id`, cardID, repo)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var x string
		if err := rows.Scan(&x); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

func (s *Store) AddPicks(cardID int64, repo string, shas []string) error {
	for _, x := range shas {
		if _, err := s.DB.Exec(`INSERT OR IGNORE INTO card_picks(card_id,repo,source_sha) VALUES(?,?,?)`, cardID, repo, x); err != nil {
			return err
		}
	}
	return nil
}

// PushReady is the push gate (A8): every repo of the ticket is integrated and none is blocked.
func (s *Store) PushReady(cardID int64) (bool, error) {
	rows, err := s.ListBranches(cardID)
	if err != nil || len(rows) == 0 {
		return false, err
	}
	for _, r := range rows {
		if !IntegratedStatus(r.Status) {
			return false, nil
		}
	}
	return true, nil
}

// RegressBranch sends an integrated repo back to "branched" after new commits landed on its feature branch.
func (s *Store) RegressBranch(cardID int64, repo string) error {
	_, err := s.DB.Exec(`UPDATE card_branches SET status='branched', note=? WHERE card_id=? AND repo=? AND status IN ('merged_dev','pushed','pr_open')`,
		"new commits after integration", cardID, repo)
	return err
}

// Stash is a git stash made for a ticket through the guard.
type Stash struct {
	ID        int64  `json:"id"`
	Repo      string `json:"repo"`
	SHA       string `json:"sha"`
	Branch    string `json:"branch"`
	Message   string `json:"message"`
	CreatedAt string `json:"createdAt"`
	PoppedAt  string `json:"poppedAt"`
}

func (s *Store) AddStash(cardID int64, repo, sha, branch, msg string) error {
	_, err := s.DB.Exec(`INSERT INTO card_stashes(card_id,repo,sha,branch,message) VALUES(?,?,?,?,?)`, cardID, repo, sha, branch, msg)
	return err
}

func (s *Store) MarkStashPopped(cardID int64, repo, sha string) error {
	_, err := s.DB.Exec(`UPDATE card_stashes SET popped_at=strftime('%Y-%m-%dT%H:%M:%SZ','now') WHERE card_id=? AND repo=? AND sha=? AND popped_at=''`, cardID, repo, sha)
	return err
}

func (s *Store) ListStashes(cardID int64) ([]Stash, error) {
	rows, err := s.DB.Query(`SELECT id,repo,sha,branch,message,created_at,popped_at FROM card_stashes WHERE card_id=? ORDER BY id DESC`, cardID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Stash{}
	for rows.Next() {
		var x Stash
		if err := rows.Scan(&x.ID, &x.Repo, &x.SHA, &x.Branch, &x.Message, &x.CreatedAt, &x.PoppedAt); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// Decision is one dated note on the ticket's decision log.
type Decision struct {
	ID        int64  `json:"id"`
	Text      string `json:"text"`
	Source    string `json:"source"` // claude | owner
	CreatedAt string `json:"createdAt"`
}

const maxDecisions = 200

func (s *Store) AddDecision(cardID int64, text, source string) (Decision, error) {
	text = strings.TrimSpace(text)
	if text == "" || len([]rune(text)) > 2000 {
		return Decision{}, ValidationError{"a decision is 1-2000 characters"}
	}
	if source != "claude" && source != "owner" {
		return Decision{}, ValidationError{"unknown source"}
	}
	var n int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM card_decisions WHERE card_id=?`, cardID).Scan(&n); err != nil {
		return Decision{}, err
	}
	if n >= maxDecisions {
		return Decision{}, ConflictError{"The decision log is full (200 notes). Delete old ones first."}
	}
	res, err := s.DB.Exec(`INSERT INTO card_decisions(card_id,text,source) VALUES(?,?,?)`, cardID, text, source)
	if err != nil {
		return Decision{}, err
	}
	id, _ := res.LastInsertId()
	var d Decision
	err = s.DB.QueryRow(`SELECT id,text,source,created_at FROM card_decisions WHERE id=?`, id).Scan(&d.ID, &d.Text, &d.Source, &d.CreatedAt)
	return d, err
}

// ListDecisions returns the log oldest first.
func (s *Store) ListDecisions(cardID int64) ([]Decision, error) {
	rows, err := s.DB.Query(`SELECT id,text,source,created_at FROM card_decisions WHERE card_id=? ORDER BY id`, cardID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Decision{}
	for rows.Next() {
		var d Decision
		if err := rows.Scan(&d.ID, &d.Text, &d.Source, &d.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) DeleteDecision(cardID, id int64) error {
	res, err := s.DB.Exec(`DELETE FROM card_decisions WHERE id=? AND card_id=?`, id, cardID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DecisionBlock formats the log for the first message of a session: newest notes that fit
// (30 notes, 6000 characters), oldest first; "" when there are none.
func DecisionBlock(ds []Decision) string {
	if len(ds) == 0 {
		return ""
	}
	start, size := len(ds), 0
	for i := len(ds) - 1; i >= 0 && len(ds)-i <= 30; i-- {
		size += len(ds[i].Text) + 24
		if size > 6000 {
			break
		}
		start = i
	}
	var b strings.Builder
	b.WriteString("Decision log (oldest first):\n")
	if start > 0 {
		b.WriteString("(" + strconv.Itoa(start) + " older notes omitted)\n")
	}
	for _, d := range ds[start:] {
		b.WriteString("- " + strings.TrimSuffix(strings.Replace(d.CreatedAt, "T", " ", 1), "Z") + " (" + d.Source + "): " + d.Text + "\n")
	}
	return b.String()
}
