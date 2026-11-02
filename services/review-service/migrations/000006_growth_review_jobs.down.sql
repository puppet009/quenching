DROP TABLE IF EXISTS growth_review_artifacts;
DROP TABLE IF EXISTS growth_review_jobs;
ALTER TABLE reviews DROP COLUMN IF EXISTS growth_report;
ALTER TABLE reviews DROP COLUMN IF EXISTS generation_prompt_tokens;
ALTER TABLE reviews DROP COLUMN IF EXISTS generation_completion_tokens;
