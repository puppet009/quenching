-- 用户能力档案：训练过程中系统计算，注册时初始化。
CREATE TABLE IF NOT EXISTS user_profiles (
    user_id           BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    collab_level      VARCHAR(10) NOT NULL DEFAULT 'C1',
    avg_collab_score  DECIMAL(5,2),
    collab_radar      JSONB,                 -- 双维雷达数据
    collab_group      VARCHAR(1),            -- A/B 对照分组，首训前才随机分配，注册时为空
    risk_notice_ack   BOOLEAN NOT NULL DEFAULT false, -- AI风险告知确认
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
