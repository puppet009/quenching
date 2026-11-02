ALTER TABLE defects ADD COLUMN provenance JSONB NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE defects ADD COLUMN template_bindings JSONB NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE defects ADD CONSTRAINT defects_bindings_array CHECK (jsonb_typeof(template_bindings)='array');
CREATE INDEX idx_defects_template_bindings ON defects USING GIN(template_bindings);
