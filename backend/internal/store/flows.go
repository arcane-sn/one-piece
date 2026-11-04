package store

import (
	"database/sql"
	"strings"
	"unicode/utf8"
)

// Flow is one Business Flow swimlane diagram (source in the swim-lanes text syntax) on a card.
type Flow struct {
	ID       int64  `json:"id"`
	CardID   int64  `json:"cardId"`
	Title    string `json:"title"`
	Source   string `json:"source"`
	Position int    `json:"position"`
	ItemID   int64  `json:"itemId"` // 0: Business Flow of the card; else Technical Flow of that item
}

func cleanFlow(title, source string) (string, string, error) {
	title = strings.TrimSpace(title)
	if n := utf8.RuneCountInString(title); n < 1 || n > 200 {
		return "", "", ValidationError{"flow title must be 1-200 characters"}
	}
	if utf8.RuneCountInString(source) > 50000 {
		return "", "", ValidationError{"flow must be at most 50000 characters"}
	}
	return title, source, nil
}

// touchCard marks the card's content as changed: new version, approval revoked.
func touchCard(tx *sql.Tx, cardID int64) error {
	res, err := tx.Exec(`UPDATE cards SET content_version=content_version+1, approved=0 WHERE id=?`, cardID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) ListFlows(cardID int64) ([]Flow, error) {
	rows, err := s.DB.Query(`SELECT id,card_id,title,source,position,item_id FROM card_flows WHERE card_id=? AND item_id=0 ORDER BY position,id`, cardID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Flow{}
	for rows.Next() {
		var f Flow
		if err := rows.Scan(&f.ID, &f.CardID, &f.Title, &f.Source, &f.Position, &f.ItemID); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (s *Store) CreateFlow(cardID int64, title, source string) (Flow, error) {
	title, source, err := cleanFlow(title, source)
	if err != nil {
		return Flow{}, err
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return Flow{}, err
	}
	defer tx.Rollback()
	if err := touchCard(tx, cardID); err != nil {
		return Flow{}, err
	}
	res, err := tx.Exec(`INSERT INTO card_flows(card_id,title,source,position)
		VALUES(?,?,?,(SELECT COALESCE(MAX(position)+1,0) FROM card_flows WHERE card_id=?))`, cardID, title, source, cardID)
	if err != nil {
		return Flow{}, err
	}
	id, _ := res.LastInsertId()
	f := Flow{ID: id, CardID: cardID, Title: title, Source: source}
	if err := tx.QueryRow(`SELECT position FROM card_flows WHERE id=?`, id).Scan(&f.Position); err != nil {
		return Flow{}, err
	}
	return f, tx.Commit()
}

func (s *Store) UpdateFlow(id int64, title, source string) (Flow, error) {
	title, source, err := cleanFlow(title, source)
	if err != nil {
		return Flow{}, err
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return Flow{}, err
	}
	defer tx.Rollback()
	var f Flow
	if err := tx.QueryRow(`SELECT card_id,position,item_id FROM card_flows WHERE id=?`, id).Scan(&f.CardID, &f.Position, &f.ItemID); err == sql.ErrNoRows {
		return Flow{}, ErrNotFound
	} else if err != nil {
		return Flow{}, err
	}
	if _, err := tx.Exec(`UPDATE card_flows SET title=?,source=? WHERE id=?`, title, source, id); err != nil {
		return Flow{}, err
	}
	if f.ItemID == 0 { // only Business Flows are part of the ticket's reviewed content
		if err := touchCard(tx, f.CardID); err != nil {
			return Flow{}, err
		}
	}
	f.ID, f.Title, f.Source = id, title, source
	return f, tx.Commit()
}

func (s *Store) DeleteFlow(id int64) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var cardID, itemID int64
	if err := tx.QueryRow(`SELECT card_id,item_id FROM card_flows WHERE id=?`, id).Scan(&cardID, &itemID); err == sql.ErrNoRows {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM card_flows WHERE id=?`, id); err != nil {
		return err
	}
	if itemID == 0 {
		if err := touchCard(tx, cardID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) GetFlow(id int64) (Flow, error) {
	var f Flow
	err := s.DB.QueryRow(`SELECT id,card_id,title,source,position,item_id FROM card_flows WHERE id=?`, id).
		Scan(&f.ID, &f.CardID, &f.Title, &f.Source, &f.Position, &f.ItemID)
	if err == sql.ErrNoRows {
		return Flow{}, ErrNotFound
	}
	return f, err
}

func (s *Store) ListItemFlows(itemID int64) ([]Flow, error) {
	rows, err := s.DB.Query(`SELECT id,card_id,title,source,position,item_id FROM card_flows WHERE item_id=? ORDER BY position,id`, itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Flow{}
	for rows.Next() {
		var f Flow
		if err := rows.Scan(&f.ID, &f.CardID, &f.Title, &f.Source, &f.Position, &f.ItemID); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// CreateItemFlow adds a Technical Flow to an Agent Task. It does not change the ticket's content version.
func (s *Store) CreateItemFlow(itemID int64, title, source string) (Flow, error) {
	title, source, err := cleanFlow(title, source)
	if err != nil {
		return Flow{}, err
	}
	it, err := s.GetItem(itemID)
	if err != nil {
		return Flow{}, err
	}
	res, err := s.DB.Exec(`INSERT INTO card_flows(card_id,title,source,position,item_id)
		VALUES(?,?,?,(SELECT COALESCE(MAX(position)+1,0) FROM card_flows WHERE item_id=?),?)`, it.CardID, title, source, itemID, itemID)
	if err != nil {
		return Flow{}, err
	}
	id, _ := res.LastInsertId()
	return s.GetFlow(id)
}
