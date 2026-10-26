package audit

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type Query struct {
	From     *time.Time
	To       *time.Time
	ActorID  *int64
	Action   Action
	Target   string
	BeforeID int64
	Limit    int
}

type Page struct {
	Items      []Record `json:"items"`
	NextBefore *int64   `json:"next_before_id,omitempty"`
}

func (s *Store) List(ctx context.Context, query Query) (Page, error) {
	if query.Limit <= 0 {
		query.Limit = 20
	}
	if query.Limit > 100 {
		return Page{}, fmt.Errorf("audit page size exceeds 100")
	}
	clauses := []string{"true"}
	args := make([]any, 0, 6)
	add := func(sql string, value any) {
		args = append(args, value)
		clauses = append(clauses, fmt.Sprintf(sql, len(args)))
	}
	if query.From != nil {
		add("ts >= $%d", query.From.UTC())
	}
	if query.To != nil {
		add("ts < $%d", query.To.UTC())
	}
	if query.ActorID != nil {
		add("actor_id = $%d", *query.ActorID)
	}
	if query.Action != "" {
		add("action = $%d", query.Action)
	}
	if query.Target != "" {
		add("target = $%d", query.Target)
	}
	if query.BeforeID > 0 {
		add("id < $%d", query.BeforeID)
	}
	args = append(args, query.Limit+1)
	statement := `SELECT id,ts,actor_id,actor_role,action,target,detail,coalesce(host(ip),''),request_id,prev_hash,hash FROM audit.log WHERE ` + strings.Join(clauses, " AND ") + fmt.Sprintf(" ORDER BY id DESC LIMIT $%d", len(args))
	rows, err := s.DB.Query(ctx, statement, args...)
	if err != nil {
		return Page{}, err
	}
	defer rows.Close()
	page := Page{Items: make([]Record, 0, query.Limit)}
	for rows.Next() {
		var record Record
		if err := rows.Scan(&record.ID, &record.Time, &record.ActorID, &record.ActorRole, &record.Action, &record.Target, &record.Detail, &record.IP, &record.RequestID, &record.PrevHash, &record.Hash); err != nil {
			return Page{}, err
		}
		safeDetail, err := sanitizeDetail(record.Detail)
		if err != nil {
			return Page{}, fmt.Errorf("sanitize stored audit detail: %w", err)
		}
		record.Detail = safeDetail
		page.Items = append(page.Items, record)
	}
	if err := rows.Err(); err != nil {
		return Page{}, err
	}
	if len(page.Items) > query.Limit {
		page.Items = page.Items[:query.Limit]
		next := page.Items[len(page.Items)-1].ID
		page.NextBefore = &next
	}
	return page, nil
}
