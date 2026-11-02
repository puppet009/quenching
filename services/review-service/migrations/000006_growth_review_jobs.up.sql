ALTER TABLE reviews ADD COLUMN growth_report jsonb;
ALTER TABLE reviews ADD COLUMN generation_prompt_tokens bigint NOT NULL DEFAULT 0;
ALTER TABLE reviews ADD COLUMN generation_completion_tokens bigint NOT NULL DEFAULT 0;
CREATE TABLE growth_review_jobs (
 review_id bigint PRIMARY KEY REFERENCES reviews(id),
 state jsonb NOT NULL,
 version bigint NOT NULL DEFAULT 1,
 phase text NOT NULL CHECK (phase IN ('analyze','reduce','synthesize','finalize','completed','failed')),
 lease_token text NOT NULL DEFAULT '',
 lease_fence bigint NOT NULL DEFAULT 0,
 lease_until timestamptz,
 retry_at timestamptz,
 updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX growth_review_jobs_due ON growth_review_jobs(retry_at,lease_until) WHERE phase NOT IN ('completed','failed');
CREATE TABLE growth_review_artifacts (
 review_id bigint NOT NULL REFERENCES growth_review_jobs(review_id),
 snapshot_hash text NOT NULL,
 phase text NOT NULL,
 level integer NOT NULL,
 batch_index integer NOT NULL,
 artifact jsonb NOT NULL,
 request_id text NOT NULL DEFAULT '',
 prompt_tokens bigint NOT NULL DEFAULT 0,
 completion_tokens bigint NOT NULL DEFAULT 0,
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (review_id,snapshot_hash,phase,level,batch_index)
);
