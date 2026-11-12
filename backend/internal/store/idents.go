package store

import (
	"database/sql"
	"fmt"
	"regexp"
)

var boardKeyRe = regexp.MustCompile(`^[A-Z]{2,5}$`)

// DeriveKey makes a 2-5 letter board key from a name that is not in taken.
func DeriveKey(name string, taken map[string]bool) string {
	var letters []byte
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c >= 'a' && c <= 'z' {
			c -= 'a' - 'A'
		}
		if c >= 'A' && c <= 'Z' {
			letters = append(letters, c)
		}
	}
	base := "BRD"
	if len(letters) >= 2 {
		base = string(letters)
	}
	for n := 3; n <= 5; n++ {
		if len(base) < n {
			break
		}
		if k := base[:n]; !taken[k] {
			return k
		}
	}
	if len(base) <= 3 && !taken[base] {
		return base
	}
	stem := base
	if len(stem) > 4 {
		stem = stem[:4]
	}
	for c := 'A'; c <= 'Z'; c++ {
		if k := stem + string(c); !taken[k] {
			return k
		}
	}
	// ponytail: 26 collisions on one stem is not realistic for one user; fall back to a scan.
	for a := 'A'; a <= 'Z'; a++ {
		for b := 'A'; b <= 'Z'; b++ {
			for c := 'A'; c <= 'Z'; c++ {
				if k := string([]rune{a, b, c}); !taken[k] {
					return k
				}
			}
		}
	}
	return "BRD"
}

func (s *Store) takenKeys(exceptID int64) (map[string]bool, error) {
	// keys of other boards, plus keys still used as the prefix of their tickets' identifiers (after a rename)
	rows, err := s.DB.Query(`SELECT key FROM boards WHERE id!=? AND key!=''
		UNION SELECT substr(ident,1,instr(ident,'-')-1) FROM cards WHERE board_id!=? AND instr(ident,'-')>0`, exceptID, exceptID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	taken := map[string]bool{}
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		taken[k] = true
	}
	return taken, rows.Err()
}

// SetBoardKey changes the prefix used for new tickets; existing identifiers are not touched.
func (s *Store) SetBoardKey(id int64, key string) (Board, error) {
	if !boardKeyRe.MatchString(key) {
		return Board{}, ValidationError{"key must be 2-5 capital letters"}
	}
	taken, err := s.takenKeys(id)
	if err != nil {
		return Board{}, err
	}
	if taken[key] {
		return Board{}, ValidationError{"that key is used by another board"}
	}
	res, err := s.DB.Exec(`UPDATE boards SET key=? WHERE id=?`, key, id)
	if err != nil {
		return Board{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return Board{}, ErrNotFound
	}
	return s.GetBoard(id)
}

// nextCardIdent bumps the board's counter and returns "{KEY}-{n}".
func nextCardIdent(tx *sql.Tx, boardID int64) (string, error) {
	var key string
	var seq int
	if err := tx.QueryRow(`SELECT key,card_seq FROM boards WHERE id=?`, boardID).Scan(&key, &seq); err != nil {
		return "", err
	}
	seq++
	if _, err := tx.Exec(`UPDATE boards SET card_seq=? WHERE id=?`, seq, boardID); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s-%d", key, seq), nil
}

// nextItemIdent bumps the card's task or test case counter and returns "{card ident}-T{n}" / "-QA{n}".
func nextItemIdent(tx *sql.Tx, cardID int64, kind string) (string, error) {
	col, tag := "task_seq", "T"
	if kind == "qa" {
		col, tag = "qa_seq", "QA"
	}
	var ident string
	var seq int
	if err := tx.QueryRow(`SELECT ident,`+col+` FROM cards WHERE id=?`, cardID).Scan(&ident, &seq); err != nil {
		return "", err
	}
	seq++
	if _, err := tx.Exec(`UPDATE cards SET `+col+`=? WHERE id=?`, seq, cardID); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s-%s%d", ident, tag, seq), nil
}

// backfillIdents gives boards, cards and items created before identifiers existed theirs. Idempotent.
func (s *Store) backfillIdents() error {
	taken, err := s.takenKeys(0)
	if err != nil {
		return err
	}
	rows, err := s.DB.Query(`SELECT id,name FROM boards WHERE key='' ORDER BY id`)
	if err != nil {
		return err
	}
	type nb struct {
		id   int64
		name string
	}
	var boards []nb
	for rows.Next() {
		var b nb
		if err := rows.Scan(&b.id, &b.name); err != nil {
			rows.Close()
			return err
		}
		boards = append(boards, b)
	}
	rows.Close()
	for _, b := range boards {
		k := DeriveKey(b.name, taken)
		taken[k] = true
		if _, err := s.DB.Exec(`UPDATE boards SET key=? WHERE id=?`, k, b.id); err != nil {
			return err
		}
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// cards: number the empty ones per board after the highest number already assigned
	if _, err := tx.Exec(`UPDATE boards SET card_seq=MAX(card_seq,(SELECT COUNT(*) FROM cards WHERE board_id=boards.id AND ident!=''))`); err != nil {
		return err
	}
	crows, err := tx.Query(`SELECT id,board_id FROM cards WHERE ident='' ORDER BY id`)
	if err != nil {
		return err
	}
	type pair struct{ id, parent int64 }
	var cards []pair
	for crows.Next() {
		var p pair
		if err := crows.Scan(&p.id, &p.parent); err != nil {
			crows.Close()
			return err
		}
		cards = append(cards, p)
	}
	crows.Close()
	for _, c := range cards {
		ident, err := nextCardIdent(tx, c.parent)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE cards SET ident=? WHERE id=?`, ident, c.id); err != nil {
			return err
		}
	}
	irows, err := tx.Query(`SELECT id,card_id,kind FROM card_items WHERE ident='' ORDER BY id`)
	if err != nil {
		return err
	}
	type item struct {
		id, card int64
		kind     string
	}
	var items []item
	for irows.Next() {
		var it item
		if err := irows.Scan(&it.id, &it.card, &it.kind); err != nil {
			irows.Close()
			return err
		}
		items = append(items, it)
	}
	irows.Close()
	for _, it := range items {
		ident, err := nextItemIdent(tx, it.card, it.kind)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE card_items SET ident=? WHERE id=?`, ident, it.id); err != nil {
			return err
		}
	}
	return tx.Commit()
}
