package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
)

// ReviewMessage is one line of a ticket's Claude Review discussion. Role is user | assistant |
// marker (a "new session" divider) | event (something Claude changed; Ref is the revision it saved).
type ReviewMessage struct {
	ID     int64  `json:"id"`
	Role   string `json:"role"`
	Text   string `json:"text"`
	Status string `json:"status"` // done | running | stopped | failed
	Ref    int64  `json:"ref"`    // event: id of the saved revision to undo to; 0 = none
}

func (s *Store) AddReviewMessage(cardID int64, role, text, status string, ref int64) (ReviewMessage, error) {
	res, err := s.DB.Exec(`INSERT INTO review_messages(card_id,role,text,status,ref) VALUES(?,?,?,?,?)`, cardID, role, text, status, ref)
	if err != nil {
		return ReviewMessage{}, err
	}
	id, _ := res.LastInsertId()
	return ReviewMessage{ID: id, Role: role, Text: text, Status: status, Ref: ref}, nil
}

// AppendReviewText adds a paragraph to a message, separated by a blank line.
func (s *Store) AppendReviewText(id int64, text string) error {
	_, err := s.DB.Exec(`UPDATE review_messages
		SET text = CASE WHEN text = '' THEN ? ELSE text || char(10) || char(10) || ? END WHERE id=?`, text, text, id)
	return err
}

func (s *Store) FinishReviewMessage(id int64, status string) error {
	_, err := s.DB.Exec(`UPDATE review_messages SET status=? WHERE id=?`, status, id)
	return err
}

func (s *Store) ListReviewMessages(cardID int64) ([]ReviewMessage, error) {
	rows, err := s.DB.Query(`SELECT id,role,text,status,ref FROM review_messages WHERE card_id=? ORDER BY id`, cardID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ReviewMessage{}
	for rows.Next() {
		var m ReviewMessage
		if err := rows.Scan(&m.ID, &m.Role, &m.Text, &m.Status, &m.Ref); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// FailRunningReviewChats marks replies left "running" by a previous server process as stopped.
func (s *Store) FailRunningReviewChats() error {
	_, err := s.DB.Exec(`UPDATE review_messages SET status='stopped' WHERE status='running'`)
	return err
}

func (s *Store) GetReviewState(cardID int64) (string, error) {
	var id string
	err := s.DB.QueryRow(`SELECT session_id FROM review_state WHERE card_id=?`, cardID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return id, err
}

func (s *Store) SetReviewState(cardID int64, sessionID string) error {
	_, err := s.DB.Exec(`INSERT INTO review_state(card_id,session_id) VALUES(?,?)
		ON CONFLICT(card_id) DO UPDATE SET session_id=excluded.session_id`, cardID, sessionID)
	return err
}

// Revision is the title and description a ticket had before Claude (or an Undo) replaced them.
type Revision struct {
	ID          int64  `json:"id"`
	CardID      int64  `json:"cardId"`
	Version     int    `json:"version"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

const keepRevisions = 20

// ReviseCard replaces a ticket's title and description the way an owner edit does (the content
// version moves on and a prior approval is revoked), after saving the old text as a Revision.
// It returns that revision's id, or 0 when nothing changed.
func (s *Store) ReviseCard(cardID int64, title, description string) (Card, int64, error) {
	title, description, err := cleanCard(title, description)
	if err != nil {
		return Card{}, 0, err
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return Card{}, 0, err
	}
	defer tx.Rollback()
	c, err := getCard(tx, cardID)
	if err != nil {
		return Card{}, 0, err
	}
	if c.Title == title && c.Description == description {
		return c, 0, nil
	}
	res, err := tx.Exec(`INSERT INTO card_revisions(card_id,version,title,description) VALUES(?,?,?,?)`, cardID, c.ContentVersion, c.Title, c.Description)
	if err != nil {
		return Card{}, 0, err
	}
	rev, _ := res.LastInsertId()
	if _, err := tx.Exec(`DELETE FROM card_revisions WHERE card_id=? AND id NOT IN (
		SELECT id FROM card_revisions WHERE card_id=? ORDER BY id DESC LIMIT ?)`, cardID, cardID, keepRevisions); err != nil {
		return Card{}, 0, err
	}
	if _, err := tx.Exec(`UPDATE cards SET title=?, description=?, content_version=content_version+1, approved=0 WHERE id=?`, title, description, cardID); err != nil {
		return Card{}, 0, err
	}
	next, err := getCard(tx, cardID)
	if err != nil {
		return Card{}, 0, err
	}
	return next, rev, tx.Commit()
}

func (s *Store) GetRevision(id int64) (Revision, error) {
	var r Revision
	err := s.DB.QueryRow(`SELECT id,card_id,version,title,description FROM card_revisions WHERE id=?`, id).
		Scan(&r.ID, &r.CardID, &r.Version, &r.Title, &r.Description)
	if errors.Is(err, sql.ErrNoRows) {
		return Revision{}, ErrNotFound
	}
	return r, err
}

// RestoreRevision puts a saved title and description back (saving the current ones as a new revision).
func (s *Store) RestoreRevision(cardID, revID int64) (Card, int64, error) {
	r, err := s.GetRevision(revID)
	if err != nil || r.CardID != cardID {
		return Card{}, 0, ErrNotFound
	}
	return s.ReviseCard(cardID, r.Title, r.Description)
}

// RecordVerdict stores a finished review of the ticket as it is right now.
func (s *Store) RecordVerdict(cardID int64, verdict, summary string, missing []string) (Review, error) {
	return s.recordVerdict(cardID, verdict, summary, missing, nil)
}

// Check is one line of the review checklist.
type Check struct {
	Key    string `json:"key"`
	Status string `json:"status"` // ok | gap | na
	Note   string `json:"note"`
}

// ReviewCriteria are the checklist keys, in display order; every verdict with a checklist covers all of them.
var ReviewCriteria = []struct{ Key, Label string }{
	{"description", "Clear description, no contradictions"},
	{"cases", "Every case (A, B, C…) has defined behaviour"},
	{"tasks", "Every requirement is covered by an Agent Task"},
	{"qa", "Every QA case traces to a requirement, and every requirement has one"},
	{"edge", "Error and edge cases are covered"},
	{"scope", "One focused change, small enough"},
	{"flows", "Business Flow matches the description"},
	{"open", "No open questions or TBD anywhere"},
}

// RecordChecklistVerdict records a verdict backed by the full checklist. Pass needs every criterion ok or na
// (never a gap); needs_work needs at least one gap or open point. Evidence, not a bare opinion.
func (s *Store) RecordChecklistVerdict(cardID int64, verdict, summary string, missing []string, checks []Check) (Review, error) {
	seen := map[string]bool{}
	gaps := 0
	for i := range checks {
		c := &checks[i]
		ok := false
		for _, k := range ReviewCriteria {
			ok = ok || k.Key == c.Key
		}
		if !ok || seen[c.Key] {
			return Review{}, ValidationError{"unknown or repeated checklist key: " + c.Key}
		}
		if c.Status != "ok" && c.Status != "gap" && c.Status != "na" {
			return Review{}, ValidationError{"checklist status must be ok, gap or na (" + c.Key + ")"}
		}
		c.Note = strings.TrimSpace(c.Note)
		if len([]rune(c.Note)) > 400 {
			c.Note = string([]rune(c.Note)[:400])
		}
		if c.Status == "gap" {
			gaps++
		}
		seen[c.Key] = true
	}
	for _, k := range ReviewCriteria {
		if !seen[k.Key] {
			return Review{}, ValidationError{"the checklist is missing: " + k.Key}
		}
	}
	switch {
	case verdict == "pass" && gaps > 0:
		return Review{}, ValidationError{"a gap in the checklist means needs_work, not pass"}
	case verdict == "needs_work" && gaps == 0 && len(missing) == 0:
		return Review{}, ValidationError{"needs_work needs a gap in the checklist or an open point"}
	}
	return s.recordVerdict(cardID, verdict, summary, missing, checks)
}

func (s *Store) recordVerdict(cardID int64, verdict, summary string, missing []string, checks []Check) (Review, error) {
	if verdict != "pass" && verdict != "needs_work" {
		return Review{}, ValidationError{"verdict must be pass or needs_work"}
	}
	c, err := getCard(s.DB, cardID)
	if err != nil {
		return Review{}, err
	}
	if missing == nil {
		missing = []string{}
	}
	if checks == nil {
		checks = []Check{}
	}
	m, _ := json.Marshal(missing)
	ck, _ := json.Marshal(checks)
	res, err := s.DB.Exec(`INSERT INTO card_reviews(card_id,content_version,status,verdict,summary,missing,checks) VALUES(?,?,'done',?,?,?,?)`,
		cardID, c.ContentVersion, verdict, strings.TrimSpace(summary), string(m), string(ck))
	if err != nil {
		return Review{}, err
	}
	id, _ := res.LastInsertId()
	return Review{ID: id, CardID: cardID, ContentVersion: c.ContentVersion, Status: "done", Verdict: verdict, Summary: summary, Missing: missing, Checks: checks, Log: []string{}}, nil
}

// ReviewSeen is what Claude last saw in the review chat, so a resumed turn only resends what changed.
func (s *Store) ReviewSeen(cardID int64) (version, decisions int) {
	_ = s.DB.QueryRow(`SELECT seen_version,seen_decisions FROM review_state WHERE card_id=?`, cardID).Scan(&version, &decisions)
	return
}

func (s *Store) SetReviewSeen(cardID int64, version, decisions int) error {
	_, err := s.DB.Exec(`INSERT INTO review_state(card_id,session_id,seen_version,seen_decisions) VALUES(?,'',?,?)
		ON CONFLICT(card_id) DO UPDATE SET seen_version=excluded.seen_version, seen_decisions=excluded.seen_decisions`, cardID, version, decisions)
	return err
}

var imgRe = regexp.MustCompile(`(?i)<img\b[^>]*?\bsrc\s*=\s*["']([^"']+)["']|!\[[^\]]*\]\(([^)\s]+)`)

// MissingImages lists the images of oldDesc that newDesc no longer contains. Claude rewrites the whole
// description, and a pasted screenshot must never disappear in the rewrite.
func MissingImages(oldDesc, newDesc string) []string {
	have := map[string]bool{}
	for _, m := range imgRe.FindAllStringSubmatch(newDesc, -1) {
		have[m[1]+m[2]] = true
	}
	var lost []string
	for _, m := range imgRe.FindAllStringSubmatch(oldDesc, -1) {
		if src := m[1] + m[2]; !have[src] {
			lost = append(lost, src)
			have[src] = true
		}
	}
	return lost
}

var swimlaneStep = regexp.MustCompile(`\S\s+(<->|-->>|->>|-->|<--|->|<-|-x|=>|<=)\s+\S`)

// CheckSwimlane rejects flow text the drawing could not show at all (no "A -> B: message" step).
func CheckSwimlane(source string) error {
	if !swimlaneStep.MatchString(source) {
		return ValidationError{`the flow needs at least one step like "Sender -> Receiver: message"`}
	}
	return nil
}
