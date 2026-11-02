ALTER TABLE brain_sessions ADD COLUMN report_requested BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE brain_sessions ADD COLUMN cleanup_reason TEXT NOT NULL DEFAULT '';
ALTER TABLE brain_sessions ADD COLUMN cleanup_lease_until TIMESTAMPTZ;
ALTER TABLE brain_sessions ADD COLUMN cleanup_retry_at TIMESTAMPTZ;
