DROP TABLE IF EXISTS review_sections;

ALTER TABLE reviews DROP COLUMN IF EXISTS retry_count;
ALTER TABLE reviews DROP COLUMN IF EXISTS error_message;
