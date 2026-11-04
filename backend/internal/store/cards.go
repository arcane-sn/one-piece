package store

import (
	"database/sql"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

type Card struct {
	ID          int64  `json:"id"`
	BoardID     int64  `json:"boardId"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Deadline    string `json:"deadline"` // "" or YYYY-MM-DD
	Approved    bool   `json:"approved"`
	// ContentVersion increases whenever title or description change; reviews are tied to it.
	ContentVersion int    `json:"contentVersion"`
	Status         string `json:"status"`
	Position       int    `json:"position"`
}

// Statuses are the kanban columns, in display order.
var Statuses = []string{"draft", "business", "agent", "qa", "done"}

func validStatus(s string) bool {
	for _, v := range Statuses {
		if v == s {
			return true
		}
	}
	return false
}

// cleanDeadline accepts "" (no deadline) or a strict YYYY-MM-DD date.
func cleanDeadline(d string) (string, error) {
	d = strings.TrimSpace(d)
	if d == "" {
		return "", nil
	}
	if t, err := time.Parse("2006-01-02", d); err != nil || t.Format("2006-01-02") != d {
		return "", ValidationError{"deadline must be a date like 2026-12-31"}
	}
	return d, nil
}

func cleanCard(title, desc string) (string, string, error) {
	title = strings.TrimSpace(title)
	if n := utf8.RuneCountInString(title); n < 1 || n > 200 {
		return "", "", ValidationError{"title must be 1-200 characters"}
	}
	if utf8.RuneCountInString(desc) > 10000 {
		return "", "", ValidationError{"description must be at most 10000 characters"}
	}
	return title, desc, nil
}

type queryer interface {
	QueryRow(query string, args ...any) *sql.Row
}

const cardCols = `id,board_id,title,description,deadline,approved,content_version,status,position`

func scanCard(row *sql.Row) (Card, error) {
	var c Card
	err := row.Scan(&c.ID, &c.BoardID, &c.Title, &c.Description, &c.Deadline, &c.Approved, &c.ContentVersion, &c.Status, &c.Position)
	if errors.Is(err, sql.ErrNoRows) {
		return Card{}, ErrNotFound
	}
	return c, err
}

func getCard(q queryer, id int64) (Card, error) {
	return scanCard(q.QueryRow(`SELECT `+cardCols+` FROM cards WHERE id=?`, id))
}

func (s *Store) CreateCard(boardID int64, title, description, deadline string) (Card, error) {
	if _, err := s.GetBoard(boardID); err != nil {
		return Card{}, err
	}
	title, description, err := cleanCard(title, description)
	if err != nil {
		return Card{}, err
	}
	if deadline, err = cleanDeadline(deadline); err != nil {
		return Card{}, err
	}
	res, err := s.DB.Exec(`INSERT INTO cards(board_id,title,description,deadline,status,position)
		VALUES(?,?,?,?,'business',(SELECT COALESCE(MAX(position)+1,0) FROM cards WHERE board_id=? AND status='business'))`,
		boardID, title, description, deadline, boardID)
	if err != nil {
		return Card{}, err
	}
	id, _ := res.LastInsertId()
	return getCard(s.DB, id)
}

func (s *Store) ListCards(boardID int64) ([]Card, error) {
	if _, err := s.GetBoard(boardID); err != nil {
		return nil, err
	}
	rows, err := s.DB.Query(`SELECT `+cardCols+` FROM cards WHERE board_id=?
		ORDER BY CASE status WHEN 'draft' THEN 0 WHEN 'business' THEN 1 WHEN 'agent' THEN 2 WHEN 'qa' THEN 3 ELSE 4 END, position`, boardID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cards := []Card{}
	for rows.Next() {
		var c Card
		if err := rows.Scan(&c.ID, &c.BoardID, &c.Title, &c.Description, &c.Deadline, &c.Approved, &c.ContentVersion, &c.Status, &c.Position); err != nil {
			return nil, err
		}
		cards = append(cards, c)
	}
	return cards, rows.Err()
}

func (s *Store) UpdateCard(id int64, title, description, deadline string) (Card, error) {
	title, description, err := cleanCard(title, description)
	if err != nil {
		return Card{}, err
	}
	if deadline, err = cleanDeadline(deadline); err != nil {
		return Card{}, err
	}
	res, err := s.DB.Exec(`UPDATE cards SET deadline=?,
			content_version = content_version + (CASE WHEN title<>? OR description<>? THEN 1 ELSE 0 END),
			approved = CASE WHEN title<>? OR description<>? THEN 0 ELSE approved END,
			title=?, description=? WHERE id=?`,
		deadline, title, description, title, description, title, description, id)
	if err != nil {
		return Card{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return Card{}, ErrNotFound
	}
	return getCard(s.DB, id)
}

// columnIDs returns card ids of a column in position order, skipping exclude.
func columnIDs(tx *sql.Tx, boardID int64, status string, exclude int64) ([]int64, error) {
	rows, err := tx.Query(`SELECT id FROM cards WHERE board_id=? AND status=? AND id<>? ORDER BY position`, boardID, status, exclude)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func renumber(tx *sql.Tx, status string, ids []int64) error {
	for i, id := range ids {
		if _, err := tx.Exec(`UPDATE cards SET status=?, position=? WHERE id=?`, status, i, id); err != nil {
			return err
		}
	}
	return nil
}

// MoveCard puts the card at index position of the target column (clamped to
// [0, len]) and renumbers both affected columns to 0..n-1.
func (s *Store) MoveCard(id int64, status string, position int) (Card, error) {
	if !validStatus(status) {
		return Card{}, ValidationError{"unknown status: " + status}
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return Card{}, err
	}
	defer tx.Rollback()
	c, err := getCard(tx, id)
	if err != nil {
		return Card{}, err
	}
	if err := checkMove(tx, c, status); err != nil {
		return Card{}, err
	}
	target, err := columnIDs(tx, c.BoardID, status, id)
	if err != nil {
		return Card{}, err
	}
	position = max(0, min(position, len(target)))
	target = append(target[:position], append([]int64{id}, target[position:]...)...)
	if err := renumber(tx, status, target); err != nil {
		return Card{}, err
	}
	if c.Status != status {
		source, err := columnIDs(tx, c.BoardID, c.Status, id)
		if err != nil {
			return Card{}, err
		}
		if err := renumber(tx, c.Status, source); err != nil {
			return Card{}, err
		}
	}
	if status == "draft" { // parked in the backlog: approval no longer holds (reviews and flows are kept)
		if _, err := tx.Exec(`UPDATE cards SET approved=0 WHERE id=?`, id); err != nil {
			return Card{}, err
		}
	}
	moved, err := getCard(tx, id)
	if err != nil {
		return Card{}, err
	}
	return moved, tx.Commit()
}

// checkMove enforces the Draft rules: Draft only leads to Business, Draft is only entered
// from Business, and a card whose review is running cannot be parked in Draft.
func checkMove(tx *sql.Tx, c Card, to string) error {
	switch {
	case c.Status == "draft" && to != "draft" && to != "business":
		return ConflictError{"A Draft ticket can only move to Business."}
	case to == "draft" && c.Status != "draft" && c.Status != "business":
		return ConflictError{"Only Business tickets can be moved back to Draft."}
	case to == "draft" && c.Status != "draft":
		var running int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM card_reviews WHERE card_id=? AND status='running'`, c.ID).Scan(&running); err != nil {
			return err
		}
		if running > 0 {
			return ConflictError{"This ticket is being reviewed. Try again when the review finishes."}
		}
	}
	return nil
}

func (s *Store) DeleteCard(id int64) error {
	res, err := s.DB.Exec(`DELETE FROM cards WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
