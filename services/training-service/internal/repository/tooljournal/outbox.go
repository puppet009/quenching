package tooljournal

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

func (s *PGStore) RunOutbox(ctx context.Context, base, token string) {
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		rows, err := s.query(ctx, `SELECT event_id,delivery FROM training_operation_events WHERE NOT delivered ORDER BY id LIMIT 100`)
		if err != nil {
			continue
		}
		var pending []Delivery
		for rows.Next() {
			var id string
			var raw []byte
			if rows.Scan(&id, &raw) != nil {
				break
			}
			var d Delivery
			if json.Unmarshal(raw, &d) == nil {
				pending = append(pending, d)
			}
		}
		rows.Close()
		for _, d := range pending {
			if Deliver(ctx, client, strings.TrimRight(base, "/"), token, d) != nil {
				break
			}
			_, _ = s.exec(ctx, `UPDATE training_operation_events SET delivered=true WHERE event_id=$1`, d.Evidence.EventID)
		}
	}
}
