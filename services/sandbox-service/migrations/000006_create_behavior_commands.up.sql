CREATE TABLE behavior_commands (
 command_id TEXT PRIMARY KEY, session_id BIGINT NOT NULL, template_slug TEXT NOT NULL,
 operation TEXT NOT NULL, parameters JSONB NOT NULL, status TEXT NOT NULL DEFAULT 'accepted',
 result JSONB, claimed_until TIMESTAMPTZ, delivered BOOLEAN NOT NULL DEFAULT false,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(), updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 CHECK(status IN ('accepted','running','succeeded','failed','unknown','cancelled'))
);
CREATE INDEX behavior_command_queue ON behavior_commands(session_id,created_at);
CREATE TABLE behavior_runner_credentials(session_id BIGINT PRIMARY KEY,token_hash TEXT NOT NULL);
