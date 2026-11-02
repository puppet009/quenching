package operationaudit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PGStore struct {
	Pool    *pgxpool.Pool
	Guard   func(context.Context, pgx.Tx, int64) error
	Local   bool
	Sandbox bool
}

func (s *PGStore) lock(ctx context.Context, tx pgx.Tx, sid int64) error {
	_, err := tx.Exec(ctx, `INSERT INTO operation_audit_sessions(session_id) VALUES($1) ON CONFLICT DO NOTHING`, sid)
	if err != nil {
		return err
	}
	var closed bool
	if err = tx.QueryRow(ctx, `SELECT sealed FROM operation_audit_sessions WHERE session_id=$1 FOR UPDATE`, sid).Scan(&closed); err != nil {
		return err
	}
	if closed {
		return ErrClosed
	}
	if s.Guard != nil {
		return s.Guard(ctx, tx, sid)
	}
	return nil
}
func (s *PGStore) Begin(ctx context.Context, sid int64, id, key, hash string, e Evidence) (bool, error) {
	if err := Validate(e); err != nil {
		return false, err
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, `INSERT INTO operation_audit_sessions(session_id) VALUES($1) ON CONFLICT DO NOTHING`, sid)
	if err != nil {
		return false, err
	}
	var sealed bool
	err = tx.QueryRow(ctx, `SELECT sealed FROM operation_audit_sessions WHERE session_id=$1 FOR UPDATE`, sid).Scan(&sealed)
	if err != nil {
		return false, err
	}
	var oldhash string
	err = tx.QueryRow(ctx, `SELECT request_hash FROM operation_intents WHERE session_id=$1 AND request_key=$2`, sid, key).Scan(&oldhash)
	if err == nil {
		if oldhash != hash {
			return false, ErrConflict
		}
		return false, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return false, err
	}
	if sealed {
		return false, ErrClosed
	}
	if s.Guard != nil {
		if err = s.Guard(ctx, tx, sid); err != nil {
			return false, err
		}
	}
	e = Sanitize(e)
	raw, _ := json.Marshal(e)
	_, err = tx.Exec(ctx, `INSERT INTO operation_intents(operation_id,session_id,request_key,request_hash,evidence) VALUES($1,$2,$3,$4,$5)`, id, sid, key, hash, raw)
	if err != nil {
		return false, err
	}
	if err = s.insertEvent(ctx, tx, e, nil); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}
func (s *PGStore) insertEvent(ctx context.Context, tx pgx.Tx, e Evidence, texts []Text) error {
	raw, _ := json.Marshal(Sanitize(e))
	d, _ := json.Marshal(Delivery{Evidence: Sanitize(e), Texts: texts})
	_, err := tx.Exec(ctx, `INSERT INTO operation_events(event_id,operation_id,session_id,sequence,evidence,delivery,delivered) VALUES($1,$2,$3,$4,$5,$6,$7)`, e.EventID, e.OperationID, e.SessionID, e.Sequence, raw, d, s.Local)
	if err != nil {
		return err
	}
	for _, t := range texts {
		if t.OperationID != e.OperationID || t.Ref != "optext:"+e.OperationID+":"+t.Slot || len(t.Content) > TextLimit {
			return ErrConflict
		}
		t.Content = RedactText(t.Content)
		t.ByteLength = len(t.Content)
		body, _ := json.Marshal(t)
		_, err = tx.Exec(ctx, `INSERT INTO operation_texts(ref,operation_id,session_id,body) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, t.Ref, e.OperationID, e.SessionID, body)
		if err != nil {
			return err
		}
	}
	return nil
}
func (s *PGStore) Append(ctx context.Context, e Evidence, texts []Text) error {
	tx, err := s.begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var raw []byte
	err = tx.QueryRow(ctx, `SELECT evidence FROM operation_intents WHERE operation_id=$1 AND session_id=$2 FOR UPDATE`, e.OperationID, e.SessionID).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	var old Evidence
	if err = json.Unmarshal(raw, &old); err != nil {
		return err
	}
	if old.EventID == e.EventID && RequestHash(old) == RequestHash(Sanitize(e)) {
		return nil
	}
	if err = ValidateTransition(old, e); err != nil {
		return err
	}
	if err = s.insertEvent(ctx, tx, e, texts); err != nil {
		return err
	}
	raw, _ = json.Marshal(Sanitize(e))
	_, err = tx.Exec(ctx, `UPDATE operation_intents SET evidence=$2,revision=$3 WHERE operation_id=$1`, e.OperationID, raw, e.Sequence)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (s *PGStore) Get(ctx context.Context, sid int64, id string) (Evidence, error) {
	var raw []byte
	var e Evidence
	err := s.queryRow(ctx, `SELECT evidence FROM operation_intents WHERE session_id=$1 AND operation_id=$2`, sid, id).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return e, ErrNotFound
	}
	if err == nil {
		err = json.Unmarshal(raw, &e)
	}
	return e, err
}
func (s *PGStore) ByKey(ctx context.Context, sid int64, key string) (Evidence, error) {
	var id string
	err := s.queryRow(ctx, `SELECT operation_id FROM operation_intents WHERE session_id=$1 AND request_key=$2`, sid, key).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Evidence{}, ErrNotFound
	}
	if err != nil {
		return Evidence{}, err
	}
	return s.Get(ctx, sid, id)
}
func (s *PGStore) List(ctx context.Context, sid, before int64, limit int) (Page, error) {
	p := Page{Items: []Evidence{}}
	if limit < 1 || limit > 50 {
		return p, fmt.Errorf("invalid limit")
	}
	rows, err := s.query(ctx, `SELECT id,evidence||jsonb_build_object('reply_message_id',coalesce((SELECT reply_message_id FROM operation_reply_links l WHERE l.operation_id=operation_events.operation_id),0)) FROM operation_events WHERE session_id=$1 AND ($2::bigint=0 OR id<$2) ORDER BY id DESC LIMIT $3`, sid, before, limit+1)
	if err != nil {
		return p, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var raw []byte
		if err = rows.Scan(&id, &raw); err != nil {
			return p, err
		}
		if len(p.Items) == limit {
			p.HasMore = true
			break
		}
		var e Evidence
		if err = json.Unmarshal(raw, &e); err != nil {
			return p, err
		}
		p.Items = append(p.Items, e)
		p.NextBefore = id
	}
	return p, rows.Err()
}
func (s *PGStore) Read(ctx context.Context, sid int64, ref string) (Text, error) {
	var raw []byte
	var t Text
	err := s.queryRow(ctx, `SELECT body FROM operation_texts WHERE session_id=$1 AND ref=$2`, sid, ref).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return t, ErrNotFound
	}
	if err == nil {
		err = json.Unmarshal(raw, &t)
	}
	return t, err
}
func (s *PGStore) Ingest(ctx context.Context, d Delivery) error {
	if err := Validate(d.Evidence); err != nil {
		return err
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	e := Sanitize(d.Evidence)
	raw, _ := json.Marshal(e)
	_, err = tx.Exec(ctx, `INSERT INTO operation_intents(operation_id,session_id,request_key,request_hash,evidence,revision) VALUES($1,$2,$1,$3,$4,$5) ON CONFLICT DO NOTHING`, e.OperationID, e.SessionID, RequestHash(raw), raw, e.Sequence)
	if err != nil {
		return err
	}
	var existing []byte
	err = tx.QueryRow(ctx, `SELECT evidence FROM operation_events WHERE event_id=$1`, e.EventID).Scan(&existing)
	if err == nil {
		var previous Evidence
		if json.Unmarshal(existing, &previous) != nil || RequestHash(previous) != RequestHash(e) {
			return ErrConflict
		}
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	var originalRaw []byte
	if err = tx.QueryRow(ctx, `SELECT evidence FROM operation_intents WHERE operation_id=$1 FOR UPDATE`, e.OperationID).Scan(&originalRaw); err != nil {
		return err
	}
	var original Evidence
	if json.Unmarshal(originalRaw, &original) != nil || original.SessionID != e.SessionID || original.Actor != e.Actor || original.Kind != e.Kind {
		return ErrConflict
	}
	if err = s.insertEvent(ctx, tx, e, d.Texts); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE operation_intents SET evidence=$2,revision=$3 WHERE operation_id=$1 AND revision<=$3`, e.OperationID, raw, e.Sequence)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
