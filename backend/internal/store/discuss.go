package store

import (
	"database/sql"
	"errors"
)

// DiscussMessage is one message of a ticket's Discuss session (Claude plans and runs the branching with the owner).
type DiscussMessage struct {
	ID     int64  `json:"id"`
	Role   string `json:"role"` // user | assistant | marker (a "new session" divider) | event (Claude changed the ticket)
	Text   string `json:"text"`
	Status string `json:"status"` // done | running | stopped | failed
	Ref    int64  `json:"ref"`    // event rows: the card revision this change can be undone to
}

func (s *Store) AddDiscussMessage(cardID int64, role, text, status string) (DiscussMessage, error) {
	res, err := s.DB.Exec(`INSERT INTO discuss_messages(card_id,role,text,status) VALUES(?,?,?,?)`, cardID, role, text, status)
	if err != nil {
		return DiscussMessage{}, err
	}
	id, _ := res.LastInsertId()
	return DiscussMessage{ID: id, Role: role, Text: text, Status: status}, nil
}

// AppendDiscussText adds a paragraph to a message, separated by a blank line.
func (s *Store) AppendDiscussText(id int64, text string) error {
	_, err := s.DB.Exec(`UPDATE discuss_messages
		SET text = CASE WHEN text = '' THEN ? ELSE text || char(10) || char(10) || ? END WHERE id=?`, text, text, id)
	return err
}

func (s *Store) FinishDiscussMessage(id int64, status string) error {
	_, err := s.DB.Exec(`UPDATE discuss_messages SET status=? WHERE id=?`, status, id)
	return err
}

func (s *Store) ListDiscussMessages(cardID int64) ([]DiscussMessage, error) {
	rows, err := s.DB.Query(`SELECT id,role,text,status,ref FROM discuss_messages WHERE card_id=? ORDER BY id`, cardID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DiscussMessage{}
	for rows.Next() {
		var m DiscussMessage
		if err := rows.Scan(&m.ID, &m.Role, &m.Text, &m.Status, &m.Ref); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// RunningDiscussTurn reports whether an assistant reply of this card is still being written.
func (s *Store) RunningDiscussTurn(cardID int64) (bool, error) {
	var n int
	err := s.DB.QueryRow(`SELECT COUNT(*) FROM discuss_messages WHERE card_id=? AND status='running'`, cardID).Scan(&n)
	return n > 0, err
}

// FailRunningDiscuss marks replies left "running" by a previous server process as stopped.
func (s *Store) FailRunningDiscuss() error {
	_, err := s.DB.Exec(`UPDATE discuss_messages SET status='stopped' WHERE status='running'`)
	return err
}

// GetDiscussState returns the Claude session id to --resume; "" means start fresh.
func (s *Store) GetDiscussState(cardID int64) (string, error) {
	var id string
	err := s.DB.QueryRow(`SELECT session_id FROM discuss_state WHERE card_id=?`, cardID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return id, err
}

func (s *Store) SetDiscussState(cardID int64, sessionID string) error {
	_, err := s.DB.Exec(`INSERT INTO discuss_state(card_id,session_id) VALUES(?,?)
		ON CONFLICT(card_id) DO UPDATE SET session_id=excluded.session_id`, cardID, sessionID)
	return err
}

// AddDiscussEvent records a change Claude made to the ticket (ref = the revision that can be restored, 0 = none).
func (s *Store) AddDiscussEvent(cardID int64, text string, ref int64) error {
	_, err := s.DB.Exec(`INSERT INTO discuss_messages(card_id,role,text,status,ref) VALUES(?,?,?,?,?)`, cardID, "event", text, "done", ref)
	return err
}

// GetDiscussSeen / SetDiscussSeen: the ticket content version Claude last saw (to resend the ticket only when it changed).
func (s *Store) GetDiscussSeen(cardID int64) int {
	var v int
	_ = s.DB.QueryRow(`SELECT seen_version FROM discuss_state WHERE card_id=?`, cardID).Scan(&v)
	return v
}

func (s *Store) SetDiscussSeen(cardID int64, v int) error {
	_, err := s.DB.Exec(`INSERT INTO discuss_state(card_id,session_id,seen_version) VALUES(?,'',?)
		ON CONFLICT(card_id) DO UPDATE SET seen_version=excluded.seen_version`, cardID, v)
	return err
}
