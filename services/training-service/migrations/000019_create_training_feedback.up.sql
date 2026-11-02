CREATE TABLE training_feedback (
 id BIGSERIAL PRIMARY KEY,
 training_id BIGINT NOT NULL REFERENCES training_sessions(id),
 user_id BIGINT NOT NULL,
 score SMALLINT CHECK(score BETWEEN 0 AND 10),
 value_feedback VARCHAR(30) CHECK(value_feedback IN ('valuable','not_valuable')),
 comment TEXT NOT NULL DEFAULT '',
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 UNIQUE(user_id,training_id)
);
CREATE INDEX idx_training_feedback_created ON training_feedback(created_at DESC,id DESC);
