-- 协作子分补齐：信息利用率。
-- ai-service EvaluateCollab 本就返回 info_utilization_score，建表迁移（000001）
-- 漏建该列，导致 training-service 回写协作明细时缺一列、对照页雷达缺信息利用率维。
ALTER TABLE reviews
    ADD COLUMN IF NOT EXISTS info_utilization_score DECIMAL(5,2);

COMMENT ON COLUMN reviews.info_utilization_score IS '协作子分：信息利用率 0-100（ai-service 评估，training-service 共库回写）';
