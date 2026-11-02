-- T4-16 复盘历史列表查询优化。
-- 列表接口固定按「某用户的未删除复盘，created_at 倒序」取页，
-- 现有 idx_reviews_user_id 仅单列索引，排序与软删过滤需回表 + Sort，
-- 建 (user_id, created_at DESC) 部分索引（与 WHERE deleted_at IS NULL 对齐），
-- 该查询可走索引顺序扫描，无需额外排序。
-- 列方向取 DESC：PG 支持索引反向扫描，但显式 DESC 与 ORDER BY 完全一致，
-- 加上同值 tie-breaker 用 id DESC，仅末尾少量行需额外比较。
CREATE INDEX IF NOT EXISTS idx_reviews_user_created
    ON reviews (user_id, created_at DESC)
    WHERE deleted_at IS NULL;
