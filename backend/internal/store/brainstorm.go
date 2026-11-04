package store

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
)

// BrainstormMessage is one chat message of a board's brainstorming conversation.
type BrainstormMessage struct {
	ID     int64  `json:"id"`
	Role   string `json:"role"` // user | assistant
	Text   string `json:"text"`
	Status string `json:"status"` // done | running | stopped | failed
}

// BrainstormState is what a board's conversation needs between turns.
type BrainstormState struct {
	SessionID string // Claude session to --resume; "" = start fresh
	SentIndex string // ticket index last shown to that session
}

// PlanVersion is one saved version of a board's plan document.
type PlanVersion struct {
	Version   int    `json:"version"`
	CreatedAt string `json:"createdAt"`
	Content   string `json:"content,omitempty"`
}

// PlanPath is where a board's plan document lives on disk, next to the database.
func (s *Store) PlanPath(boardID int64) string {
	return filepath.Join(s.Dir, "plans", fmt.Sprintf("board-%d.md", boardID))
}

func (s *Store) AddBrainstormMessage(boardID int64, role, text, status string) (BrainstormMessage, error) {
	res, err := s.DB.Exec(`INSERT INTO brainstorm_messages(board_id,role,text,status) VALUES(?,?,?,?)`, boardID, role, text, status)
	if err != nil {
		return BrainstormMessage{}, err
	}
	id, _ := res.LastInsertId()
	return BrainstormMessage{ID: id, Role: role, Text: text, Status: status}, nil
}

// AppendBrainstormText adds a paragraph to a message, separated by a blank line.
func (s *Store) AppendBrainstormText(id int64, text string) error {
	_, err := s.DB.Exec(`UPDATE brainstorm_messages
		SET text = CASE WHEN text = '' THEN ? ELSE text || char(10) || char(10) || ? END WHERE id=?`, text, text, id)
	return err
}

func (s *Store) FinishBrainstormMessage(id int64, status string) error {
	_, err := s.DB.Exec(`UPDATE brainstorm_messages SET status=? WHERE id=?`, status, id)
	return err
}

func (s *Store) ListBrainstormMessages(boardID int64) ([]BrainstormMessage, error) {
	rows, err := s.DB.Query(`SELECT id,role,text,status FROM brainstorm_messages WHERE board_id=? ORDER BY id`, boardID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BrainstormMessage{}
	for rows.Next() {
		var m BrainstormMessage
		if err := rows.Scan(&m.ID, &m.Role, &m.Text, &m.Status); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// RunningBrainstormTurn reports whether an assistant reply of this board is still being written.
func (s *Store) RunningBrainstormTurn(boardID int64) (bool, error) {
	var n int
	err := s.DB.QueryRow(`SELECT COUNT(*) FROM brainstorm_messages WHERE board_id=? AND status='running'`, boardID).Scan(&n)
	return n > 0, err
}

// FailRunningBrainstorm marks replies left "running" by a previous server process as stopped.
func (s *Store) FailRunningBrainstorm() error {
	_, err := s.DB.Exec(`UPDATE brainstorm_messages SET status='stopped' WHERE status='running'`)
	return err
}

func (s *Store) GetBrainstormState(boardID int64) (BrainstormState, error) {
	var st BrainstormState
	err := s.DB.QueryRow(`SELECT session_id,sent_index FROM brainstorm_state WHERE board_id=?`, boardID).Scan(&st.SessionID, &st.SentIndex)
	if errors.Is(err, sql.ErrNoRows) {
		return BrainstormState{}, nil
	}
	return st, err
}

func (s *Store) SetBrainstormState(boardID int64, st BrainstormState) error {
	_, err := s.DB.Exec(`INSERT INTO brainstorm_state(board_id,session_id,sent_index) VALUES(?,?,?)
		ON CONFLICT(board_id) DO UPDATE SET session_id=excluded.session_id, sent_index=excluded.sent_index`,
		boardID, st.SessionID, st.SentIndex)
	return err
}

func (s *Store) AddPlanVersion(boardID int64, content string) (PlanVersion, error) {
	res, err := s.DB.Exec(`INSERT INTO plan_versions(board_id,version,content)
		VALUES(?,(SELECT COALESCE(MAX(version),0)+1 FROM plan_versions WHERE board_id=?),?)`, boardID, boardID, content)
	if err != nil {
		return PlanVersion{}, err
	}
	id, _ := res.LastInsertId()
	var v PlanVersion
	err = s.DB.QueryRow(`SELECT version,created_at,content FROM plan_versions WHERE id=?`, id).Scan(&v.Version, &v.CreatedAt, &v.Content)
	return v, err
}

// ListPlanVersions returns the versions newest first, without their content.
func (s *Store) ListPlanVersions(boardID int64) ([]PlanVersion, error) {
	rows, err := s.DB.Query(`SELECT version,created_at FROM plan_versions WHERE board_id=? ORDER BY version DESC`, boardID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PlanVersion{}
	for rows.Next() {
		var v PlanVersion
		if err := rows.Scan(&v.Version, &v.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) GetPlanVersion(boardID int64, v int) (PlanVersion, error) {
	var pv PlanVersion
	err := s.DB.QueryRow(`SELECT version,created_at,content FROM plan_versions WHERE board_id=? AND version=?`, boardID, v).
		Scan(&pv.Version, &pv.CreatedAt, &pv.Content)
	if errors.Is(err, sql.ErrNoRows) {
		return PlanVersion{}, ErrNotFound
	}
	return pv, err
}

func (s *Store) LatestPlanVersion(boardID int64) (PlanVersion, error) {
	var pv PlanVersion
	err := s.DB.QueryRow(`SELECT version,created_at,content FROM plan_versions WHERE board_id=? ORDER BY version DESC LIMIT 1`, boardID).
		Scan(&pv.Version, &pv.CreatedAt, &pv.Content)
	if errors.Is(err, sql.ErrNoRows) {
		return PlanVersion{}, ErrNotFound
	}
	return pv, err
}
