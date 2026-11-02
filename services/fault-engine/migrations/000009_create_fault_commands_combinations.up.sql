CREATE TABLE fault_commands (
 command_id TEXT PRIMARY KEY, session_id BIGINT NOT NULL CHECK(session_id>0), intent JSONB NOT NULL, result JSONB NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX fault_commands_session_idx ON fault_commands(session_id);
CREATE TABLE fault_combinations (
 id BIGSERIAL PRIMARY KEY, template_slug TEXT NOT NULL,
 first_defect_id BIGINT NOT NULL REFERENCES defects(id),second_defect_id BIGINT NOT NULL REFERENCES defects(id),
 configuration JSONB NOT NULL,CHECK(first_defect_id<second_defect_id),UNIQUE(template_slug,first_defect_id,second_defect_id)
);
