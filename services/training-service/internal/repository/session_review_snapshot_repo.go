package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	oe "github.com/quenching/proto/operationevidence"
	rc "github.com/quenching/proto/reviewcontract"
	"github.com/quenching/training-service/internal/service"
	"time"
)

type SessionReviewSnapshotRepo struct {
	pool   *pgxpool.Pool
	inputs service.ReviewInputStore
}

func NewSessionReviewSnapshotRepo(p *pgxpool.Pool, in service.ReviewInputStore) *SessionReviewSnapshotRepo {
	return &SessionReviewSnapshotRepo{p, in}
}

var _ service.SnapshotStore = (*SessionReviewSnapshotRepo)(nil)

type reviewQuery interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func readEligibility(ctx context.Context, q reviewQuery, u, s, p int64, lock bool) (rc.Eligibility, error) {
	var e rc.Eligibility
	query := `SELECT s.id,s.user_id,s.project_id,s.status,coalesce(b.phase,''),coalesce(b.cleanup_status,'') FROM training_sessions s LEFT JOIN brain_sessions b ON b.session_id=s.id WHERE s.id=$1 AND s.user_id=$2 AND s.deleted_at IS NULL`
	if lock {
		query += ` FOR UPDATE OF s`
	}
	err := q.QueryRow(ctx, query, s, u).Scan(&e.SessionID, &e.UserID, &e.ProjectID, &e.SessionStatus, &e.BrainPhase, &e.CleanupStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return e, service.ErrReviewSourceNotFound
	}
	if err != nil {
		return e, err
	}
	return service.CheckReviewEligibility(e, u, s, p)
}
func (r *SessionReviewSnapshotRepo) Eligibility(ctx context.Context, u, s, p int64) (rc.Eligibility, error) {
	return readEligibility(ctx, r.pool, u, s, p, false)
}
func frozenManifest(ctx context.Context, q reviewQuery, u, s int64) (int64, rc.Snapshot, error) {
	var id int64
	var raw []byte
	var snap rc.Snapshot
	err := q.QueryRow(ctx, `SELECT id,manifest FROM session_review_snapshots WHERE user_id=$1 AND session_id=$2`, u, s).Scan(&id, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, snap, service.ErrReviewSourceNotFound
	}
	if err != nil {
		return 0, snap, err
	}
	err = json.Unmarshal(raw, &snap)
	return id, snap, err
}
func (r *SessionReviewSnapshotRepo) Manifest(ctx context.Context, u, s int64) (rc.Snapshot, error) {
	_, snap, err := frozenManifest(ctx, r.pool, u, s)
	if err != nil {
		return snap, err
	}
	e, err := r.Eligibility(ctx, u, s, snap.ProjectID)
	if err != nil {
		return rc.Snapshot{}, err
	}
	if !e.CanGenerate {
		return rc.Snapshot{}, service.ErrReviewSessionActive
	}
	return snap, nil
}
func (r *SessionReviewSnapshotRepo) Freeze(ctx context.Context, u, s, p int64) (rc.Snapshot, error) {
	eligible, err := r.Eligibility(ctx, u, s, p)
	if err != nil {
		return rc.Snapshot{}, err
	}
	if !eligible.CanGenerate {
		return rc.Snapshot{}, service.ErrReviewSessionActive
	}
	if _, snap, e := frozenManifest(ctx, r.pool, u, s); e == nil {
		return snap, nil
	} else if !errors.Is(e, service.ErrReviewSourceNotFound) {
		return snap, e
	}
	if r.inputs == nil {
		return rc.Snapshot{}, errors.New("review fault input unavailable")
	}
	input, err := r.inputs.GetReviewInput(ctx, s)
	if err != nil {
		return rc.Snapshot{}, err
	}
	if input == nil {
		return rc.Snapshot{}, service.ErrReviewSourceNotFound
	}
	// Lock the aggregate before reading originals. READ COMMITTED sees a concurrent committed freeze after waiting.
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return rc.Snapshot{}, err
	}
	defer tx.Rollback(ctx)
	eligible, err = readEligibility(ctx, tx, u, s, p, true)
	if err != nil {
		return rc.Snapshot{}, err
	}
	if !eligible.CanGenerate {
		return rc.Snapshot{}, service.ErrReviewSessionActive
	}
	if _, snap, e := frozenManifest(ctx, tx, u, s); e == nil {
		return snap, nil
	} else if !errors.Is(e, service.ErrReviewSourceNotFound) {
		return snap, e
	}
	if eligible.BrainPhase != "" {
		var phase, cleanup string
		err = tx.QueryRow(ctx, `SELECT phase,cleanup_status FROM brain_sessions WHERE session_id=$1 FOR UPDATE`, s).Scan(&phase, &cleanup)
		if err != nil {
			return rc.Snapshot{}, err
		}
		eligible.BrainPhase = phase
		eligible.CleanupStatus = cleanup
		eligible, err = service.CheckReviewEligibility(eligible, u, s, p)
		if err != nil || !eligible.CanGenerate {
			return rc.Snapshot{}, service.ErrReviewSessionActive
		}
	}
	sources := []rc.Source{}
	seen := map[string]bool{}
	urls := []string{}
	knownURLs := map[string]bool{}
	var evidenceWM int64
	add := func(src rc.Source) {
		if !seen[src.ID] {
			seen[src.ID] = true
			sources = append(sources, src)
		}
	}
	rows, err := tx.Query(ctx, `SELECT m.id,m.role,m.content,m.created_at FROM ai_messages m JOIN ai_conversations c ON c.id=m.conversation_id WHERE c.user_id=$1 AND c.session_id=$2 AND c.conversation_type='training_assistant' ORDER BY m.id`, u, s)
	if err != nil {
		return rc.Snapshot{}, err
	}
	for rows.Next() {
		var id int64
		var src rc.Source
		err = rows.Scan(&id, &src.Role, &src.Text, &src.OccurredAt)
		if err != nil {
			rows.Close()
			return rc.Snapshot{}, err
		}
		src.ID = fmt.Sprintf("message:%d", id)
		src.Kind = "message"
		add(src)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return rc.Snapshot{}, err
	}
	// Copy raw operations before their normalized brain mirrors; user-submitted outcomes remain statements.
	for _, audit := range []struct{ kind, actor, prefix, query string }{
		{"operation", "student_report", "operation:", `SELECT id,to_jsonb(o)::text,created_at FROM training_operation_projection o WHERE session_id=$1 AND deleted_at IS NULL ORDER BY id`},
		{"assistant_file_change", "platform", "file-change:", `SELECT id,jsonb_build_object('id',id,'t',EXTRACT(EPOCH FROM (created_at - (SELECT created_at FROM training_sessions WHERE id=$1)))::int,'performed_by','assistant','path',path,'pod',pod,'container',container,'before_existed',before_existed,'before_content',before_content,'after_content',after_content)::text,created_at FROM ai_change_records WHERE session_id=$1 AND deleted_at IS NULL ORDER BY id`},
	} {
		auditRows, e := tx.Query(ctx, audit.query, s)
		if e != nil {
			return rc.Snapshot{}, e
		}
		for auditRows.Next() {
			var id int64
			var raw string
			var at time.Time
			if e = auditRows.Scan(&id, &raw, &at); e != nil {
				auditRows.Close()
				return rc.Snapshot{}, e
			}
			src, e := reviewAuditSource(fmt.Sprint(audit.prefix, id), audit.kind, audit.actor, raw, at)
			if e != nil {
				auditRows.Close()
				return rc.Snapshot{}, e
			}
			add(src)
		}
		e = auditRows.Err()
		auditRows.Close()
		if e != nil {
			return rc.Snapshot{}, e
		}
	}
	rows, err = tx.Query(ctx, `SELECT e.id,e.sequence,e.kind,e.actor,e.payload::text,e.occurred_at,coalesce((SELECT CASE WHEN count(*)=1 THEN 'challenge:'||min(c.challenge_no)::text ELSE '' END FROM brain_challenges c WHERE c.session_id=e.session_id AND c.evidence_refs ? e.id),'') FROM brain_evidence_refs e WHERE e.session_id=$1 AND e.kind<>'operation_evidence' AND e.actor IN ('student','student_report','simulator') ORDER BY e.sequence`, s)
	if err != nil {
		return rc.Snapshot{}, err
	}
	for rows.Next() {
		var id, kind, actor, raw, challenge string
		var sequence int64
		var at time.Time
		err = rows.Scan(&id, &sequence, &kind, &actor, &raw, &at, &challenge)
		if err != nil {
			rows.Close()
			return rc.Snapshot{}, err
		}
		v := publicMemoryRawSource(id, kind, actor, raw, at, challenge)
		var payload map[string]any
		_ = json.Unmarshal([]byte(raw), &payload)
		incomplete := payload["truncated"] == true || payload["execution_status"] == "unknown"
		add(rc.Source{ID: v.ID, Kind: v.Kind, Actor: actor, Role: v.Role, Text: v.Text, OccurredAt: at, ChallengeID: challenge, Successful: v.Successful, Incomplete: incomplete})
		evidenceWM = sequence
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return rc.Snapshot{}, err
	}
	// Only public business/simulator execution receipts are admitted; director plans and tool parameters stay private.
	verificationRows, e := tx.Query(ctx, `SELECT command_id,action,result::text,updated_at,coalesce((SELECT CASE WHEN count(*)=1 THEN 'challenge:'||min(c.challenge_no)::text ELSE '' END FROM brain_challenges c WHERE c.session_id=t.session_id AND c.evidence_refs ? ('tool:'||t.command_id)),'') FROM brain_tool_executions t WHERE session_id=$1 AND action IN ('verify_business','run_user_behavior') AND status IN ('succeeded','failed') ORDER BY updated_at,command_id`, s)
	if e != nil {
		return rc.Snapshot{}, e
	}
	for verificationRows.Next() {
		var command, action, raw, challenge string
		var at time.Time
		if e = verificationRows.Scan(&command, &action, &raw, &at, &challenge); e != nil {
			verificationRows.Close()
			return rc.Snapshot{}, e
		}
		src, e := reviewAuditSource("tool:"+command, "verification", "platform", raw, at)
		if e != nil {
			verificationRows.Close()
			return rc.Snapshot{}, e
		}
		src.ChallengeID = challenge
		add(src)
	}
	e = verificationRows.Err()
	verificationRows.Close()
	if e != nil {
		return rc.Snapshot{}, e
	}
	for _, spec := range []struct{ kind, query string }{
		{"challenge", `SELECT challenge_no,to_jsonb(c)::text FROM brain_challenges c WHERE session_id=$1 ORDER BY challenge_no`},
		{"assessment", `SELECT version,assessment::text FROM ability_assessments WHERE session_id=$1 ORDER BY version`},
	} {
		rows, err = tx.Query(ctx, spec.query, s)
		if err != nil {
			return rc.Snapshot{}, err
		}
		for rows.Next() {
			var id int64
			var raw string
			if err = rows.Scan(&id, &raw); err != nil {
				rows.Close()
				return rc.Snapshot{}, err
			}
			challenge := ""
			if spec.kind == "challenge" {
				challenge = fmt.Sprintf("challenge:%d", id)
			}
			add(rc.Source{ID: fmt.Sprintf("%s:%d", spec.kind, id), Kind: spec.kind, Actor: "platform", Text: raw, ChallengeID: challenge})
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return rc.Snapshot{}, err
		}
	}
	for _, script := range input.Scripts {
		raw, _ := json.Marshal(script)
		add(rc.Source{ID: fmt.Sprintf("fault:%d", script.DefectID), Kind: "fault", Text: string(raw)})
	}
	// Template project0 receives fault details/provenance from the accepted injection receipt.
	rows, err = tx.Query(ctx, `SELECT result->>'result_json' FROM brain_tool_executions WHERE session_id=$1 AND action='inject_fault' AND status='succeeded' ORDER BY updated_at`, s)
	if err != nil {
		return rc.Snapshot{}, err
	}
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			rows.Close()
			return rc.Snapshot{}, err
		}
		var body struct {
			Faults []map[string]any `json:"faults"`
		}
		if err = json.Unmarshal([]byte(raw), &body); err != nil {
			rows.Close()
			return rc.Snapshot{}, err
		}
		for _, fault := range body.Faults {
			id, _ := fault["defect_id"].(float64)
			if id == 0 {
				id, _ = fault["id"].(float64)
			}
			if id <= 0 {
				rows.Close()
				return rc.Snapshot{}, errors.New("fault receipt has no identity")
			}
			encoded, _ := json.Marshal(fault)
			add(rc.Source{ID: fmt.Sprintf("fault:%d", int64(id)), Kind: "fault", Text: string(encoded)})
			if provenance, ok := fault["provenance"].(map[string]any); ok {
				if url, ok := provenance["url"].(string); ok && url != "" && !knownURLs[url] {
					knownURLs[url] = true
					urls = append(urls, url)
				}
			}
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return rc.Snapshot{}, err
	}
	operationEvents, operationSources, operationSeals, err := freezeOperations(ctx, tx, s)
	if err != nil {
		return rc.Snapshot{}, err
	}
	for _, src := range operationSources {
		add(src)
	}
	metadata, _ := json.Marshal(map[string]any{"difficulty": input.Difficulty, "fault_type": input.FaultType})
	snap, sources, err := service.BuildReviewSnapshot(eligible, sources, metadata, time.Now())
	if err != nil {
		return snap, err
	}
	service.ApplyOperationCoverage(&snap, operationEvents, operationSeals)
	service.ApplyPublicSourceGaps(&snap, sources)
	snap.EvidenceWatermark = evidenceWM
	snap.SourceURLs = urls
	service.SetReviewSnapshotHash(&snap, sources)
	encoded, _ := json.Marshal(snap)
	var snapshotID int64
	err = tx.QueryRow(ctx, `INSERT INTO session_review_snapshots(session_id,user_id,project_id,manifest) VALUES($1,$2,$3,$4) RETURNING id`, s, u, p, encoded).Scan(&snapshotID)
	if err != nil {
		return snap, err
	}
	for _, src := range sources {
		encoded, _ = json.Marshal(src)
		if _, err = tx.Exec(ctx, `INSERT INTO session_review_sources(snapshot_id,ordinal,source_id,source) VALUES($1,$2,$3,$4)`, snapshotID, src.Ordinal, src.ID, encoded); err != nil {
			return snap, err
		}
	}
	err = tx.Commit(ctx)
	return snap, err
}
func (r *SessionReviewSnapshotRepo) Read(ctx context.Context, u, s, version int64, ref string, offset, limit int) (rc.Fragment, error) {
	snap, err := r.Manifest(ctx, u, s)
	if err != nil {
		return rc.Fragment{}, err
	}
	if snap.Version != version {
		return rc.Fragment{}, service.ErrReviewSourceNotFound
	}
	var raw []byte
	err = r.pool.QueryRow(ctx, `SELECT src.source FROM session_review_sources src JOIN session_review_snapshots sn ON sn.id=src.snapshot_id WHERE sn.user_id=$1 AND sn.session_id=$2 AND sn.version=$3 AND src.source_id=$4`, u, s, version, ref).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return rc.Fragment{}, service.ErrReviewSourceNotFound
	}
	if err != nil {
		return rc.Fragment{}, err
	}
	var src rc.Source
	if err = json.Unmarshal(raw, &src); err != nil {
		return rc.Fragment{}, err
	}
	return service.ReviewSourceFragment(src, offset, limit)
}
func (r *SessionReviewSnapshotRepo) Sources(ctx context.Context, u, s, version int64, c rc.Cursor, budget int) (rc.SourcePage, error) {
	out := rc.SourcePage{Fragments: []rc.Fragment{}, Next: c}
	snap, err := r.Manifest(ctx, u, s)
	if err != nil {
		return out, err
	}
	if version != snap.Version {
		return out, service.ErrReviewSourceNotFound
	}
	if c.Ordinal == 0 {
		c.Ordinal = 1
		out.Next = c
	}
	if c.Ordinal < 1 || c.Ordinal > snap.SourceCount+1 || c.OffsetBytes < 0 || budget < 512 || budget > 48*1024 {
		return out, service.ErrReviewSourceRange
	}
	if c.Ordinal == snap.SourceCount+1 {
		if c.OffsetBytes != 0 {
			return out, service.ErrReviewSourceRange
		}
		out.Done = true
		return out, nil
	}
	rows, err := r.pool.Query(ctx, `SELECT src.source FROM session_review_sources src JOIN session_review_snapshots sn ON sn.id=src.snapshot_id WHERE sn.user_id=$1 AND sn.session_id=$2 AND sn.version=$3 AND src.ordinal>=$4 ORDER BY src.ordinal`, u, s, version, c.Ordinal)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	remaining := budget
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			return out, err
		}
		var src rc.Source
		if err = json.Unmarshal(raw, &src); err != nil {
			return out, err
		}
		offset := 0
		if src.Ordinal == c.Ordinal {
			offset = c.OffsetBytes
		}
		for {
			f, e := service.BudgetReviewSourceFragment(src, offset, remaining-1)
			if e != nil {
				if len(out.Fragments) > 0 {
					return out, nil
				}
				return out, e
			}
			encoded, _ := json.Marshal(f)
			if len(encoded) > remaining {
				if len(out.Fragments) == 0 {
					return out, errors.New("fragment exceeds requested page budget")
				}
				return out, nil
			}
			remaining -= len(encoded) + 1
			out.Fragments = append(out.Fragments, f)
			if f.Complete {
				out.Next = rc.Cursor{Ordinal: src.Ordinal + 1}
				break
			}
			offset = f.NextOffsetBytes
			out.Next = rc.Cursor{Ordinal: src.Ordinal, OffsetBytes: offset}
		}
	}
	if err = rows.Err(); err != nil {
		return out, err
	}
	out.Done = out.Next.Ordinal > snap.SourceCount
	return out, nil
}

func freezeOperations(ctx context.Context, tx pgx.Tx, sid int64) ([]oe.Evidence, []rc.Source, []oe.AuditSeal, error) {
	var events []oe.Evidence
	var sources []rc.Source
	var seals []oe.AuditSeal
	rows, err := tx.Query(ctx, `SELECT evidence FROM training_operation_events WHERE session_id=$1 ORDER BY id`, sid)
	if err != nil {
		return nil, nil, nil, err
	}
	for rows.Next() {
		var raw []byte
		var e oe.Evidence
		if err = rows.Scan(&raw); err == nil {
			err = json.Unmarshal(raw, &e)
		}
		if err != nil {
			rows.Close()
			return nil, nil, nil, err
		}
		events = append(events, e)
		sources = append(sources, service.OperationReviewSource(e))
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, nil, nil, err
	}
	rows, err = tx.Query(ctx, `SELECT t.body,i.evidence FROM training_operation_texts t JOIN training_operation_intents i USING(operation_id) WHERE t.session_id=$1 ORDER BY t.ref`, sid)
	if err != nil {
		return nil, nil, nil, err
	}
	for rows.Next() {
		var body, raw []byte
		var t oe.Text
		var e oe.Evidence
		if err = rows.Scan(&body, &raw); err == nil {
			err = json.Unmarshal(body, &t)
		}
		if err == nil {
			err = json.Unmarshal(raw, &e)
		}
		if err != nil {
			rows.Close()
			return nil, nil, nil, err
		}
		sources = append(sources, rc.Source{ID: t.Ref, Kind: "operation_text", Actor: e.Actor, Text: t.Content, OccurredAt: e.StartedAt, Incomplete: t.Coverage.Output != "complete"})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, nil, nil, err
	}
	var state string
	var watermark int64
	var gaps []byte
	err = tx.QueryRow(ctx, `SELECT seal_state,watermark,gaps FROM training_tool_audit_sessions WHERE session_id=$1`, sid).Scan(&state, &watermark, &gaps)
	if err == nil {
		seal := oe.AuditSeal{SessionID: sid, Producer: "training", State: state, Watermark: watermark}
		_ = json.Unmarshal(gaps, &seal.Gaps)
		seals = append(seals, seal)
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, nil, err
	}
	var remote []byte
	err = tx.QueryRow(ctx, `SELECT seal FROM training_tool_remote_seals WHERE session_id=$1`, sid).Scan(&remote)
	if err == nil {
		var seal oe.AuditSeal
		if err = json.Unmarshal(remote, &seal); err != nil {
			return nil, nil, nil, err
		}
		seals = append(seals, seal)
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, nil, err
	}
	return events, sources, seals, nil
}
