package store

import (
	"database/sql"
	"encoding/json"
	"strings"
	"unicode/utf8"
)

// Item is an Agent Task (kind "agent") or QA Test Case (kind "qa") of a card.
type Item struct {
	ID          int64    `json:"id"`
	CardID      int64    `json:"cardId"`
	Kind        string   `json:"kind"`
	Ident       string   `json:"ident"` // e.g. MID-12-T3; generated, never changes
	Title       string   `json:"title"`
	Body        string   `json:"body"`
	Position    int      `json:"position"`
	Status      string   `json:"status"` // todo | doing | done
	Log         []string `json:"log"`
	Version     int      `json:"version"`     // ticket version it was generated from; 0 = added by hand
	Failure     string   `json:"failure"`     // QA: the one-line issue of the last failed run; "" when it has not failed
	FailureDesc string   `json:"failureDesc"` // QA: the detailed explanation of that failure
	FixFor      int64    `json:"fixFor"`      // Agent: the failed QA item this [FIX] task is for; 0 otherwise
	Superseded  bool     `json:"superseded"`  // struck through: replaced by a newer Generate, kept for history
	Running     bool     `json:"running"`     // Claude is working on it right now; set by the API, never stored
}

const itemCols = `id,card_id,kind,ident,title,body,position,status,log,version,failure,failure_desc,fix_for,superseded`

func scanItem(sc interface{ Scan(...any) error }) (Item, error) {
	var i Item
	var log string
	if err := sc.Scan(&i.ID, &i.CardID, &i.Kind, &i.Ident, &i.Title, &i.Body, &i.Position, &i.Status, &log, &i.Version, &i.Failure, &i.FailureDesc, &i.FixFor, &i.Superseded); err != nil {
		return Item{}, err
	}
	if json.Unmarshal([]byte(log), &i.Log) != nil || i.Log == nil {
		i.Log = []string{}
	}
	return i, nil
}

func cleanItem(kind, title, body string) (string, string, error) {
	title = strings.TrimSpace(title)
	if kind != "agent" && kind != "qa" {
		return "", "", ValidationError{"kind must be agent or qa"}
	}
	if n := utf8.RuneCountInString(title); n < 1 || n > 200 {
		return "", "", ValidationError{"title must be 1-200 characters"}
	}
	if utf8.RuneCountInString(body) > 10000 {
		return "", "", ValidationError{"body must be at most 10000 characters"}
	}
	return title, body, nil
}

func (s *Store) ListItems(cardID int64) ([]Item, error) {
	rows, err := s.DB.Query(`SELECT `+itemCols+` FROM card_items WHERE card_id=? ORDER BY kind,superseded,position,id`, cardID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Item{}
	for rows.Next() {
		i, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

func (s *Store) GetItem(id int64) (Item, error) {
	i, err := scanItem(s.DB.QueryRow(`SELECT `+itemCols+` FROM card_items WHERE id=?`, id))
	if err == sql.ErrNoRows {
		return Item{}, ErrNotFound
	}
	return i, err
}

func insertItem(tx *sql.Tx, cardID int64, kind, title, body string, log []string, version int) (Item, error) {
	if log == nil {
		log = []string{}
	}
	lj, _ := json.Marshal(log)
	ident, err := nextItemIdent(tx, cardID, kind)
	if err != nil {
		return Item{}, err
	}
	res, err := tx.Exec(`INSERT INTO card_items(card_id,kind,ident,title,body,position,log,version)
		VALUES(?,?,?,?,?,(SELECT COALESCE(MAX(position)+1,0) FROM card_items WHERE card_id=? AND kind=?),?,?)`, cardID, kind, ident, title, body, cardID, kind, string(lj), version)
	if err != nil {
		return Item{}, err
	}
	id, _ := res.LastInsertId()
	it := Item{ID: id, CardID: cardID, Kind: kind, Ident: ident, Title: title, Body: body, Status: "todo", Log: log, Version: version}
	err = tx.QueryRow(`SELECT position FROM card_items WHERE id=?`, id).Scan(&it.Position)
	return it, err
}

func (s *Store) CreateItem(cardID int64, kind, title, body string) (Item, error) {
	title, body, err := cleanItem(kind, title, body)
	if err != nil {
		return Item{}, err
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return Item{}, err
	}
	defer tx.Rollback()
	var n int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM cards WHERE id=?`, cardID).Scan(&n); err != nil {
		return Item{}, err
	}
	if n == 0 {
		return Item{}, ErrNotFound
	}
	it, err := insertItem(tx, cardID, kind, title, body, nil, 0)
	if err != nil {
		return Item{}, err
	}
	return it, tx.Commit()
}

func (s *Store) UpdateItem(id int64, title, body string) (Item, error) {
	it, err := s.GetItem(id)
	if err != nil {
		return Item{}, err
	}
	if title, body, err = cleanItem(it.Kind, title, body); err != nil {
		return Item{}, err
	}
	if _, err := s.DB.Exec(`UPDATE card_items SET title=?,body=? WHERE id=?`, title, body, id); err != nil {
		return Item{}, err
	}
	it.Title, it.Body = title, body
	return it, nil
}

func (s *Store) DeleteItem(id int64) error {
	s.DB.Exec("DELETE FROM card_flows WHERE item_id=?", id) // its Technical Flows go with it
	s.DeleteJobsFor("item", id)
	res, err := s.DB.Exec(`DELETE FROM card_items WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SupersedeAndAdd strikes through a card's To Do items of one kind (nothing is deleted) and
// appends the given ones. Items already doing or done, and [FIX] tasks, stay active.
func (s *Store) SupersedeAndAdd(cardID int64, kind string, items []Item) ([]Item, error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE card_items SET superseded=1, log=json_insert(log,'$[#]','Diganti oleh generate baru')
		WHERE card_id=? AND kind=? AND fix_for=0 AND status='todo' AND superseded=0`, cardID, kind); err != nil {
		return nil, err
	}
	out := []Item{}
	for _, in := range items {
		title, body, err := cleanItem(kind, in.Title, in.Body)
		if err != nil {
			return nil, err
		}
		it, err := insertItem(tx, cardID, kind, title, body, in.Log, in.Version)
		if err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, tx.Commit()
}

// SetSuperseded strikes an item through (true) or brings it back (false).
func (s *Store) SetSuperseded(id int64, v bool) (Item, error) {
	n := 0
	if v {
		n = 1
	}
	res, err := s.DB.Exec(`UPDATE card_items SET superseded=? WHERE id=?`, n, id)
	if err != nil {
		return Item{}, err
	}
	if c, _ := res.RowsAffected(); c == 0 {
		return Item{}, ErrNotFound
	}
	return s.GetItem(id)
}

// SaveItem updates title, body, status and appends log lines in one transaction.
// An empty status keeps the current one.
func (s *Store) SaveItem(id int64, title, body, status string, logLines []string) (Item, error) {
	it, err := s.GetItem(id)
	if err != nil {
		return Item{}, err
	}
	if title, body, err = cleanItem(it.Kind, title, body); err != nil {
		return Item{}, err
	}
	if status == "" {
		status = it.Status
	}
	if status != "todo" && status != "doing" && status != "done" {
		return Item{}, ValidationError{"status must be todo, doing or done"}
	}
	it.Log = append(it.Log, logLines...)
	if len(it.Log) > 200 {
		it.Log = it.Log[len(it.Log)-200:]
	}
	lj, _ := json.Marshal(it.Log)
	if _, err := s.DB.Exec(`UPDATE card_items SET title=?,body=?,status=?,log=? WHERE id=?`, title, body, status, string(lj), id); err != nil {
		return Item{}, err
	}
	it.Title, it.Body, it.Status = title, body, status
	return it, nil
}

// SetItemStatus moves an item between todo, doing and done.
func (s *Store) SetItemStatus(id int64, status string) (Item, error) {
	if status != "todo" && status != "doing" && status != "done" {
		return Item{}, ValidationError{"status must be todo, doing or done"}
	}
	if _, err := s.DB.Exec(`UPDATE card_items SET status=? WHERE id=?`, status, id); err != nil {
		return Item{}, err
	}
	return s.GetItem(id)
}

// AppendItemLog adds one line to the item's Claude log (capped at 200 lines).
func (s *Store) AppendItemLog(id int64, line string) error {
	it, err := s.GetItem(id)
	if err != nil {
		return err
	}
	it.Log = append(it.Log, line)
	if len(it.Log) > 200 {
		it.Log = it.Log[len(it.Log)-200:]
	}
	lj, _ := json.Marshal(it.Log)
	_, err = s.DB.Exec(`UPDATE card_items SET log=? WHERE id=?`, string(lj), id)
	return err
}

// SetItemFailure records why a QA item failed: a one-line issue and a detailed description
// (both "" clear it after a pass).
func (s *Store) SetItemFailure(id int64, issue, desc string) error {
	_, err := s.DB.Exec(`UPDATE card_items SET failure=?, failure_desc=? WHERE id=?`, issue, desc, id)
	return err
}

// AddFixTask creates the "[FIX] <test case>" Agent Task for a failed QA item, unless one
// for it is still open (not Done). The second result reports whether one was created.
func (s *Store) AddFixTask(qa Item, issue, desc string) (Item, bool, error) {
	var open, earlier int
	if err := s.DB.QueryRow(`SELECT COALESCE(SUM(status!='done'),0), COUNT(*) FROM card_items WHERE kind='agent' AND fix_for=?`, qa.ID).Scan(&open, &earlier); err != nil {
		return Item{}, false, err
	}
	if open > 0 {
		return Item{}, false, nil
	}
	body := "Test case QA gagal: " + qa.Title + "\n\n"
	if earlier > 0 {
		body += "Perbaikan sebelumnya belum berhasil: test case ini masih gagal.\n\n"
	}
	body += "Issue: " + issue + "\n\nDesc: " + desc + "\n\nLangkah dan hasil yang diharapkan:\n" + qa.Body +
		"\n\nPerbaiki kodenya supaya test case ini lolos."
	title := "[FIX] " + qa.Title
	if n := utf8.RuneCountInString(title); n > 200 {
		title = string([]rune(title)[:200])
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return Item{}, false, err
	}
	defer tx.Rollback()
	it, err := insertItem(tx, qa.CardID, "agent", title, body, []string{"Created because QA failed: " + qa.Title}, 0)
	if err != nil {
		return Item{}, false, err
	}
	if _, err := tx.Exec(`UPDATE card_items SET fix_for=? WHERE id=?`, qa.ID, it.ID); err != nil {
		return Item{}, false, err
	}
	it.FixFor = qa.ID
	return it, true, tx.Commit()
}
