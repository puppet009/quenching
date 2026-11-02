-- T4-16 复盘/查询优化：补齐两条热路径的复合索引。
--
-- 1) 复盘时间线（review-service review_input_repo.queryTimeline）：
--      WHERE session_id = ? AND deleted_at IS NULL
--      ORDER BY virtual_timestamp NULLS LAST, id
--    现有 idx_session_operations_session_time 是 (session_id, created_at)，
--    与排序键不一致，复盘取数要全量回表 + Sort。
--    新索引按真实排序键建（virtual_timestamp 可空，必须显式 NULLS LAST 才与
--    ORDER BY 对齐、支持索引顺序扫描）；保留旧索引——training-service 自身
--    的坑点运行时（pitfall_runtime）按 created_at 范围取数仍在用它。
CREATE INDEX IF NOT EXISTS idx_session_operations_session_vts
    ON session_operations (session_id, virtual_timestamp NULLS LAST, id)
    WHERE deleted_at IS NULL;

-- 2) 复盘坑点章节（review-service pitfall_repo.CollectSessionPitfalls）：
--      JOIN ai_conversations 后 WHERE m.is_pitfall = true AND m.role = 'assistant'
--      ORDER BY m.created_at, m.id
--    坑点消息只占全表极小比例，建带谓词的部分索引：体积小、命中率高。
--    谓词与查询完全一致（is_pitfall=true AND role='assistant'），
--    前置 created_at, id 直接满足排序；首列 conversation_id 便于未来
--    「单会话坑点」直查走索引（复盘 JOIN 以 conversation_id 关联，
--    规划器也可选用该索引做嵌套循环内表）。
CREATE INDEX IF NOT EXISTS idx_ai_messages_pitfall_conv
    ON ai_messages (conversation_id, created_at, id)
    WHERE is_pitfall = true AND role = 'assistant';
