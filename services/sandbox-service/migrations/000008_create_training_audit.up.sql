CREATE TABLE sandbox_training_audit (
 id TEXT PRIMARY KEY,session_id BIGINT NOT NULL,kind TEXT NOT NULL,event JSONB NOT NULL,
 delivered BOOLEAN NOT NULL DEFAULT false,created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX sandbox_audit_pending ON sandbox_training_audit(created_at) WHERE NOT delivered;
