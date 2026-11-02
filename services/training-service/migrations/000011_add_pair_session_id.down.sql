DROP INDEX IF EXISTS idx_training_sessions_user_group_pair;
DROP INDEX IF EXISTS idx_training_sessions_pair_session_id;
ALTER TABLE training_sessions DROP COLUMN IF EXISTS pair_session_id;
