-- T3-24 双维评分：补齐技术分与修复结论的持久化列。
-- GetSessionScoreDetail（GET /api/v1/training/sessions/:id/score）早已读取
-- tech_score / fault_resolved，但建表迁移（000003）漏建这两列，真实 PG 上
-- 该查询会报 undefined column。本迁移补齐，使只读接口可用，并接通会话结束评分回写。
ALTER TABLE training_sessions
    ADD COLUMN IF NOT EXISTS tech_score     DECIMAL(6,2),
    ADD COLUMN IF NOT EXISTS fault_resolved BOOLEAN;

COMMENT ON COLUMN training_sessions.tech_score IS '技术分 0-100（修复/耗时/操作代价/提示四维加权），未评分时 NULL';
COMMENT ON COLUMN training_sessions.fault_resolved IS '本次训练是否成功修复故障；NULL 表示尚未有评分结论';
