-- T3-25 人机对照交叉分组：两次训练会话互相配对（《技术架构文档》§5.2）。
-- model.Session.PairSessionID 早已预留，此处补持久化列。
-- 配对在应用层保证双向互指且同组、模式互补；跨用户无引用意义，不建物理外键，仅建索引。
ALTER TABLE training_sessions
    ADD COLUMN IF NOT EXISTS pair_session_id BIGINT;

CREATE INDEX IF NOT EXISTS idx_training_sessions_pair_session_id
    ON training_sessions (pair_session_id);
-- 找"某用户某组尚未配对的首场会话"是第二次训练编排的高频查询。
CREATE INDEX IF NOT EXISTS idx_training_sessions_user_group_pair
    ON training_sessions (user_id, control_group, pair_session_id);
