DROP INDEX IF EXISTS idx_defects_template_bindings;
ALTER TABLE defects DROP COLUMN IF EXISTS template_bindings;
ALTER TABLE defects DROP COLUMN IF EXISTS provenance;
