DROP INDEX brain_one_active_challenge;
ALTER TABLE brain_challenges DROP COLUMN expires_at;
ALTER TABLE brain_tool_executions DROP COLUMN applied;
ALTER TABLE brain_sessions DROP COLUMN baseline_operations;
