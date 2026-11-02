DROP INDEX IF EXISTS idx_sandbox_active_session;
DROP INDEX IF EXISTS idx_sandbox_allocation_queue;
ALTER TABLE sandboxes DROP COLUMN IF EXISTS allocation_reason,DROP COLUMN IF EXISTS resources_admitted,DROP COLUMN IF EXISTS resource_plan,DROP COLUMN IF EXISTS template_digest,DROP COLUMN IF EXISTS template_slug;
