package store

import (
	"database/sql"
	"errors"
	"strings"
	"unicode/utf8"
)

type Card struct {
	ID          int64  `json:"id"`
	BoardID     int64  `json:"boardId"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Status      string `json:"status"`
	Position    int    `json:"position"`
}

// Statuses are the kanban columns, in display order.
var Statuses = []string{"business", "agent", "qa", "done"}

func validStatus(s string) bool {
	for _, v := range Statuses {
		if v == s {
			return true
		}
	}
	return false
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

const cardCols = `id,board_id,title,description,status,position`

func scanCard(row *sql.Row) (Card, error) {
	var c Card
	err := row.Scan(&c.ID, &c.BoardID, &c.Title, &c.Description, &c.Status, &c.Position)
	if errors.Is(err, sql.ErrNoRows) {
		return Card{}, ErrNotFound
	}
	return c, err
}

func getCard(q queryer, id int64) (Card, error) {
	return scanCard(q.QueryRow(`SELECT `+cardCols+` FROM cards WHERE id=?`, id))
}

func (s *Store) CreateCard(boardID int64, title, description string) (Card, error) {
	if _, err := s.GetBoard(boardID); err != nil {
		return Card{}, err
	}
	title, description, err := cleanCard(title, description)
	if err != nil {
		return Card{}, err
	}
	res, err := s.DB.Exec(`INSERT INTO cards(board_id,title,description,status,position)
		VALUES(?,?,?,'business',(SELECT COALESCE(MAX(position)+1,0) FROM cards WHERE board_id=? AND status='business'))`,
		boardID, title, description, boardID)
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
		ORDER BY CASE status WHEN 'business' THEN 0 WHEN 'agent' THEN 1 WHEN 'qa' THEN 2 ELSE 3 END, position`, boardID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cards := []Card{}
	for rows.Next() {
		var c Card
		if err := rows.Scan(&c.ID, &c.BoardID, &c.Title, &c.Description, &c.Status, &c.Position); err != nil {
			return nil, err
		}
		cards = append(cards, c)
	}
	return cards, rows.Err()
}

func (s *Store) UpdateCard(id int64, title, description string) (Card, error) {
	title, description, err := cleanCard(title, description)
	if err != nil {
		return Card{}, err
	}
	res, err := s.DB.Exec(`UPDATE cards SET title=?, description=? WHERE id=?`, title, description, id)
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
	moved, err := getCard(tx, id)
	if err != nil {
		return Card{}, err
	}
	return moved, tx.Commit()
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
