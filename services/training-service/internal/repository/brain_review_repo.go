package repository

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
)

// Read only frozen sessions; the private director inputs never enter active student APIs.
func (r *BrainRepo) ReviewBrainInput(ctx context.Context, sid int64) (json.RawMessage, error) {
	var phase, cleanup string
	err := r.pool.QueryRow(ctx, `SELECT phase,cleanup_status FROM brain_sessions WHERE session_id=$1`, sid).Scan(&phase, &cleanup)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if phase != "closing" && phase != "reviewing" && phase != "completed" && phase != "failed" {
		return nil, errors.New("brain session must be frozen before review")
	}
	if cleanup == "pending" {
		return nil, errors.New("waiting for sandbox audit delivery and cleanup")
	}
	var raw []byte
	err = r.pool.QueryRow(ctx, `SELECT jsonb_build_object(
 'evidence',coalesce((SELECT jsonb_agg(jsonb_build_object('id',e.id,'kind',e.kind,'actor',e.actor,'payload',e.payload,'occurred_at',e.occurred_at) ORDER BY e.sequence) FROM brain_evidence_refs e WHERE e.session_id=b.session_id),'[]'::jsonb),
 'challenges',coalesce((SELECT jsonb_agg(to_jsonb(c) ORDER BY c.challenge_no) FROM brain_challenges c WHERE c.session_id=b.session_id),'[]'::jsonb),
 'assessment',(SELECT assessment FROM ability_assessments WHERE session_id=b.session_id ORDER BY version DESC LIMIT 1),
 'sources','[]'::jsonb,'cleanup_status',b.cleanup_status)
 FROM brain_sessions b WHERE b.session_id=$1`, sid).Scan(&raw)
	if err != nil {
		return nil, err
	}
	var body map[string]any
	if err = json.Unmarshal(raw, &body); err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx, `SELECT result->>'result_json' FROM brain_tool_executions WHERE session_id=$1 AND action='inject_fault' AND status='succeeded' ORDER BY updated_at`, sid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	sources := []string{}
	faults := []any{}
	seen := map[string]bool{}
	for rows.Next() {
		var result string
		if err = rows.Scan(&result); err != nil {
			return nil, err
		}
		var v struct {
			Faults []map[string]any `json:"faults"`
		}
		if err = json.Unmarshal([]byte(result), &v); err != nil {
			return nil, err
		}
		for _, f := range v.Faults {
			faults = append(faults, f)
			if provenance, ok := f["provenance"].(map[string]any); ok {
				if url, ok := provenance["url"].(string); ok && url != "" && !seen[url] {
					seen[url] = true
					sources = append(sources, url)
				}
			}
		}
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	body["sources"] = sources
	body["faults"] = faults
	return json.Marshal(body)
}
