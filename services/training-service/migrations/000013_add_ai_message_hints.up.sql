-- T3-5 渐进式提示：在 ai_messages 上标记助手提示消息及其级别。
-- is_hint=true 表示该条助手消息消耗了一次提示配额；hint_level 记录提示级别
-- （1 方向引导 / 2 缩小范围 / 3 接近答案，NULL=普通应答）。
-- 提示配额按训练会话、跨对话统计（与坑点注入计数口径一致）：
--   ai_messages m JOIN ai_conversations c ON c.id=m.conversation_id
--   WHERE c.session_id=$1 AND m.is_hint=true
-- 会话对话数很少，走 idx_ai_conversations_session_id + ai_messages 主键即可，
-- 无需额外跨表索引。
ALTER TABLE ai_messages ADD COLUMN IF NOT EXISTS is_hint   BOOLEAN  NOT NULL DEFAULT false;
ALTER TABLE ai_messages ADD COLUMN IF NOT EXISTS hint_level SMALLINT;
