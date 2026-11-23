package store

import "database/sql"

// UsageEvent is the token use and cost of one Claude turn for one model.
type UsageEvent struct {
	TS                                   int64 // unix seconds
	Model                                string
	Input, Output, CacheRead, CacheWrite int64
	Cost                                 float64
}

// UsageSum is UsageEvent totals for one model.
type UsageSum struct {
	Model      string  `json:"model"`
	Input      int64   `json:"input"`
	Output     int64   `json:"output"`
	CacheRead  int64   `json:"cacheRead"`
	CacheWrite int64   `json:"cacheWrite"`
	Cost       float64 `json:"costUsd"`
	Turns      int64   `json:"turns"`
}

// UsageLimit is one account rate-limit window as last reported by the Claude CLI.
type UsageLimit struct {
	Utilization float64 `json:"utilization"` // 0..1
	ResetsAt    int64   `json:"resetsAt"`    // unix seconds
	Status      string  `json:"status"`
	UpdatedAt   int64   `json:"updatedAt"`
}

func (s *Store) AddUsage(e UsageEvent) error {
	_, err := s.DB.Exec(`INSERT INTO usage_events(ts,model,input,output,cache_read,cache_write,cost) VALUES(?,?,?,?,?,?,?)`,
		e.TS, e.Model, e.Input, e.Output, e.CacheRead, e.CacheWrite, e.Cost)
	return err
}

// SumUsage totals events since the given unix time, per model, biggest spender first.
func (s *Store) SumUsage(since int64) ([]UsageSum, error) {
	rows, err := s.DB.Query(`SELECT model,SUM(input),SUM(output),SUM(cache_read),SUM(cache_write),SUM(cost),COUNT(*)
		FROM usage_events WHERE ts>=? GROUP BY model ORDER BY SUM(cost) DESC, model`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []UsageSum{}
	for rows.Next() {
		var u UsageSum
		if err := rows.Scan(&u.Model, &u.Input, &u.Output, &u.CacheRead, &u.CacheWrite, &u.Cost, &u.Turns); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) SetUsageLimit(name string, l UsageLimit) error {
	_, err := s.DB.Exec(`INSERT INTO usage_limits(name,utilization,resets_at,status,updated_at) VALUES(?,?,?,?,?)
		ON CONFLICT(name) DO UPDATE SET utilization=excluded.utilization,resets_at=excluded.resets_at,status=excluded.status,updated_at=excluded.updated_at`,
		name, l.Utilization, l.ResetsAt, l.Status, l.UpdatedAt)
	return err
}

func (s *Store) GetUsageLimit(name string) (*UsageLimit, error) {
	var l UsageLimit
	err := s.DB.QueryRow(`SELECT utilization,resets_at,status,updated_at FROM usage_limits WHERE name=?`, name).
		Scan(&l.Utilization, &l.ResetsAt, &l.Status, &l.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &l, err
}

func (s *Store) SetUsageModel(model string) error {
	_, err := s.DB.Exec(`INSERT INTO usage_meta(k,v) VALUES('model',?) ON CONFLICT(k) DO UPDATE SET v=excluded.v`, model)
	return err
}

func (s *Store) GetUsageModel() string {
	var m string
	_ = s.DB.QueryRow(`SELECT v FROM usage_meta WHERE k='model'`).Scan(&m)
	return m
}
