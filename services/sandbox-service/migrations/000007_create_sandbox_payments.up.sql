CREATE TABLE sandbox_payments (
 command_id TEXT PRIMARY KEY, session_id BIGINT NOT NULL, order_id TEXT NOT NULL, account_ref TEXT NOT NULL,
 amount_minor BIGINT NOT NULL CHECK(amount_minor>0), currency TEXT NOT NULL, receipt_id TEXT NOT NULL,
 status TEXT NOT NULL DEFAULT 'pending', reason TEXT NOT NULL DEFAULT '', updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 UNIQUE(session_id,order_id), CHECK(status IN ('pending','running','succeeded','failed','unknown','cancelled'))
);
