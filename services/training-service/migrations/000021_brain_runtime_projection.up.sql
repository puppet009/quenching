ALTER TABLE brain_sessions ADD COLUMN baseline_operations JSONB NOT NULL DEFAULT '{}';
ALTER TABLE brain_tool_executions ADD COLUMN applied BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE brain_challenges ADD COLUMN expires_at TIMESTAMPTZ;
CREATE UNIQUE INDEX brain_one_active_challenge ON brain_challenges(session_id) WHERE status='active';
