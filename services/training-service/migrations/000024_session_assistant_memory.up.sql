CREATE TABLE session_assistant_identity (
 user_id BIGINT NOT NULL, session_id BIGINT NOT NULL REFERENCES training_sessions(id),
 conversation_id BIGINT NOT NULL UNIQUE REFERENCES ai_conversations(id),
 lease_token TEXT NOT NULL DEFAULT '', fence BIGINT NOT NULL DEFAULT 0, lease_expires TIMESTAMPTZ,
 memory_version BIGINT NOT NULL DEFAULT 0,
 PRIMARY KEY(user_id,session_id)
);
INSERT INTO session_assistant_identity(user_id,session_id,conversation_id)
 SELECT c.user_id,c.session_id,min(c.id) FROM ai_conversations c JOIN training_sessions s ON s.id=c.session_id AND s.user_id=c.user_id
 WHERE c.conversation_type='training_assistant' AND s.deleted_at IS NULL GROUP BY c.user_id,c.session_id;
CREATE TABLE session_assistant_memories (
 user_id BIGINT NOT NULL, session_id BIGINT NOT NULL, version BIGINT NOT NULL,
 schema_version INT NOT NULL DEFAULT 1, message_watermark BIGINT NOT NULL DEFAULT 0,
 evidence_watermark BIGINT NOT NULL DEFAULT 0, memory JSONB NOT NULL,
 actual_model TEXT NOT NULL DEFAULT '', prompt_tokens BIGINT NOT NULL DEFAULT 0, completion_tokens BIGINT NOT NULL DEFAULT 0,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(), PRIMARY KEY(user_id,session_id,version),
 FOREIGN KEY(user_id,session_id) REFERENCES session_assistant_identity(user_id,session_id)
);
CREATE INDEX ai_messages_conversation_id_cursor ON ai_messages(conversation_id,id);
