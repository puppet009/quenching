ALTER TABLE sandboxes ADD COLUMN IF NOT EXISTS template_slug text NOT NULL DEFAULT '';
ALTER TABLE sandboxes ADD COLUMN IF NOT EXISTS template_digest text NOT NULL DEFAULT '';
ALTER TABLE sandboxes ADD COLUMN IF NOT EXISTS resource_plan jsonb NOT NULL DEFAULT '{}';
ALTER TABLE sandboxes ADD COLUMN IF NOT EXISTS resources_admitted boolean NOT NULL DEFAULT false;
ALTER TABLE sandboxes ADD COLUMN IF NOT EXISTS allocation_reason text NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_sandbox_allocation_queue ON sandboxes(created_at,id) WHERE status IN ('queued','deploying') AND deleted_at IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_sandbox_active_session ON sandboxes(session_id) WHERE session_id IS NOT NULL AND deleted_at IS NULL AND status NOT IN ('released','failed');
