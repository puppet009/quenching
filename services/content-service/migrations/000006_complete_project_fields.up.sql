-- The project API already reads/writes these fields. Existing installations
-- only have the minimal table from 000001; add defaults without rewriting data.
ALTER TABLE projects
    ADD COLUMN IF NOT EXISTS description TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS architecture_image TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS difficulty VARCHAR(20) NOT NULL DEFAULT 'medium';
