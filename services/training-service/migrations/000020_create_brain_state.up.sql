-- Training domain only. Runtime resources may be deleted without deleting audit.
CREATE TABLE brain_sessions (
 session_id BIGINT PRIMARY KEY REFERENCES training_sessions(id),
 phase VARCHAR(40) NOT NULL DEFAULT 'sandbox_ready', observation_version BIGINT NOT NULL DEFAULT 0,
 lease_owner TEXT NOT NULL DEFAULT '', lease_version BIGINT NOT NULL DEFAULT 0, lease_until TIMESTAMPTZ,
 baseline_verified BOOLEAN NOT NULL DEFAULT false, current_challenge INT NOT NULL DEFAULT 0,
 hint_level INT NOT NULL DEFAULT 0, hint_budget INT NOT NULL DEFAULT 3,
 reason TEXT NOT NULL DEFAULT '', cleanup_status TEXT NOT NULL DEFAULT 'pending',
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE brain_events (
 session_id BIGINT NOT NULL REFERENCES brain_sessions(session_id), source TEXT NOT NULL, event_id TEXT NOT NULL,
 kind TEXT NOT NULL, evidence_refs JSONB NOT NULL DEFAULT '[]', processed BOOLEAN NOT NULL DEFAULT false,
 sequence BIGSERIAL UNIQUE, created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 PRIMARY KEY(session_id,source,event_id)
);
CREATE TABLE brain_decisions (
 decision_id TEXT PRIMARY KEY, session_id BIGINT NOT NULL REFERENCES brain_sessions(session_id),
 observation_version BIGINT NOT NULL, decision JSONB NOT NULL, actual_model TEXT NOT NULL,
 request_id TEXT NOT NULL DEFAULT '', prompt_version TEXT NOT NULL, prompt_tokens BIGINT NOT NULL DEFAULT 0,
 completion_tokens BIGINT NOT NULL DEFAULT 0, latency_ms DOUBLE PRECISION NOT NULL DEFAULT 0,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(), UNIQUE(session_id,observation_version)
);
CREATE TABLE brain_tool_executions (
 command_id TEXT PRIMARY KEY, session_id BIGINT NOT NULL REFERENCES brain_sessions(session_id),
 decision_id TEXT NOT NULL REFERENCES brain_decisions(decision_id), observation_version BIGINT NOT NULL,
 domain TEXT NOT NULL, action TEXT NOT NULL, parameters JSONB NOT NULL,
 status TEXT NOT NULL DEFAULT 'pending', result JSONB, attempts INT NOT NULL DEFAULT 0,
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now(), CHECK(status IN ('pending','accepted','running','succeeded','failed','unknown','cancelled'))
);
CREATE INDEX brain_pending_commands ON brain_tool_executions(session_id,updated_at) WHERE status IN ('pending','accepted','running','unknown');
CREATE TABLE brain_evidence_refs (
 id TEXT PRIMARY KEY, session_id BIGINT NOT NULL REFERENCES brain_sessions(session_id), sequence BIGSERIAL UNIQUE,
 kind TEXT NOT NULL, actor TEXT NOT NULL, occurred_at TIMESTAMPTZ NOT NULL,
 payload JSONB NOT NULL, parent_decision_id TEXT NOT NULL DEFAULT '', created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX brain_evidence_session_seq ON brain_evidence_refs(session_id,sequence);
CREATE TABLE ability_assessments (
 session_id BIGINT NOT NULL REFERENCES brain_sessions(session_id), version BIGINT NOT NULL,
 assessment JSONB NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT now(), PRIMARY KEY(session_id,version)
);
CREATE TABLE brain_challenges (
 session_id BIGINT NOT NULL REFERENCES brain_sessions(session_id), challenge_no INT NOT NULL,
 status TEXT NOT NULL DEFAULT 'planned', learning_objective TEXT NOT NULL,
 defect_ids JSONB NOT NULL DEFAULT '[]', experiment_uids JSONB NOT NULL DEFAULT '[]', evidence_refs JSONB NOT NULL DEFAULT '[]',
 accepted_digest TEXT NOT NULL DEFAULT '', started_at TIMESTAMPTZ, completed_at TIMESTAMPTZ,
 PRIMARY KEY(session_id,challenge_no)
);
