package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
)

// Review is one Claude review of a card's content at a given content version.
type Review struct {
	ID             int64    `json:"id"`
	CardID         int64    `json:"cardId"`
	ContentVersion int      `json:"contentVersion"`
	Status         string   `json:"status"`  // running | done | failed
	Verdict        string   `json:"verdict"` // pass | needs_work (when done)
	Summary        string   `json:"summary"`
	Missing        []string `json:"missing"`
	Log            []string `json:"log"`
	Error          string   `json:"error"`
}

func (s *Store) StartReview(cardID int64, version int) (Review, error) {
	res, err := s.DB.Exec(`INSERT INTO card_reviews(card_id,content_version,status) VALUES(?,?,'running')`, cardID, version)
	if err != nil {
		return Review{}, err
	}
	id, _ := res.LastInsertId()
	return Review{ID: id, CardID: cardID, ContentVersion: version, Status: "running", Missing: []string{}, Log: []string{}}, nil
}

// AppendReviewLog adds one line to the live log (lines are single-line, capped at 200 per review).
func (s *Store) AppendReviewLog(id int64, line string) error {
	line = strings.TrimSpace(strings.NewReplacer("\n", " ", "\r", " ").Replace(line))
	if line == "" {
		return nil
	}
	_, err := s.DB.Exec(`UPDATE card_reviews SET log = log || ? || char(10)
		WHERE id=? AND (length(log) - length(replace(log, char(10), ''))) < 200`, line, id)
	return err
}

func (s *Store) FinishReview(id int64, verdict, summary string, missing []string) error {
	if missing == nil {
		missing = []string{}
	}
	m, _ := json.Marshal(missing)
	_, err := s.DB.Exec(`UPDATE card_reviews SET status='done', verdict=?, summary=?, missing=? WHERE id=?`, verdict, summary, string(m), id)
	return err
}

func (s *Store) FailReview(id int64, msg string) error {
	_, err := s.DB.Exec(`UPDATE card_reviews SET status='failed', error=? WHERE id=?`, msg, id)
	return err
}

// FailRunningReviews marks reviews left "running" by a previous server process as failed.
func (s *Store) FailRunningReviews() error {
	_, err := s.DB.Exec(`UPDATE card_reviews SET status='failed', error='Server restarted during the review.' WHERE status='running'`)
	return err
}

// LatestReview returns the newest review of a card, or ErrNotFound.
func (s *Store) LatestReview(cardID int64) (Review, error) {
	var r Review
	var missing, log string
	err := s.DB.QueryRow(`SELECT id,card_id,content_version,status,verdict,summary,missing,log,error
		FROM card_reviews WHERE card_id=? ORDER BY id DESC LIMIT 1`, cardID).
		Scan(&r.ID, &r.CardID, &r.ContentVersion, &r.Status, &r.Verdict, &r.Summary, &missing, &log, &r.Error)
	if errors.Is(err, sql.ErrNoRows) {
		return Review{}, ErrNotFound
	}
	if err != nil {
		return Review{}, err
	}
	r.Missing = []string{}
	json.Unmarshal([]byte(missing), &r.Missing)
	r.Log = []string{}
	if log != "" {
		r.Log = strings.Split(strings.TrimSuffix(log, "\n"), "\n")
	}
	return r, nil
}

// SetApproved approves (only with a passing review of the current content) or revokes approval.
func (s *Store) SetApproved(cardID int64, approved bool) (Card, error) {
	c, err := getCard(s.DB, cardID)
	if err != nil {
		return Card{}, err
	}
	if approved {
		if c.Status == "draft" {
			return Card{}, ConflictError{"Draft tickets can't be approved. Move it to Business first."}
		}
		r, err := s.LatestReview(cardID)
		if errors.Is(err, ErrNotFound) || (err == nil && (r.Status != "done" || r.Verdict != "pass" || r.ContentVersion != c.ContentVersion)) {
			return Card{}, ConflictError{"Approval needs a passing Claude review of the current ticket."}
		}
		if err != nil {
			return Card{}, err
		}
	}
	v := 0
	if approved {
		v = 1
	}
	if _, err := s.DB.Exec(`UPDATE cards SET approved=? WHERE id=?`, v, cardID); err != nil {
		return Card{}, err
	}
	return getCard(s.DB, cardID)
}

// GetCard returns one card by id, or ErrNotFound.
func (s *Store) GetCard(id int64) (Card, error) { return getCard(s.DB, id) }
