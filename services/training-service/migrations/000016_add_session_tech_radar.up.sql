-- 双维评分接线：技术维雷达快照。
-- tech_score 只有标量总分，个人中心/人机对照页需要 6 维技术雷达，
-- 会话结束评分时由 service.BuildTechRadar 生成并随技术分一并落库。
-- JSON 结构与 model.RadarData / 前端 radar.ts 对齐：{"dimensions":[...6 维],"values":[...0-100]}。
ALTER TABLE training_sessions
    ADD COLUMN IF NOT EXISTS tech_radar JSONB;

COMMENT ON COLUMN training_sessions.tech_radar IS '技术维雷达快照 JSON：{dimensions,values}，6 维顺序 fault_diagnosis/tool_proficiency/info_retrieval/operation_cost/verification/time_efficiency';
