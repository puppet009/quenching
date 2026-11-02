CREATE TABLE session_review_snapshots (
 id BIGSERIAL PRIMARY KEY,session_id BIGINT NOT NULL UNIQUE,user_id BIGINT NOT NULL,project_id BIGINT NOT NULL,
 version BIGINT NOT NULL DEFAULT 1 CHECK(version=1),manifest JSONB NOT NULL,created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE session_review_sources (
 snapshot_id BIGINT NOT NULL REFERENCES session_review_snapshots(id) ON DELETE CASCADE,
 ordinal BIGINT NOT NULL CHECK(ordinal>0),source_id TEXT NOT NULL,source JSONB NOT NULL,
 PRIMARY KEY(snapshot_id,ordinal),UNIQUE(snapshot_id,source_id)
);
