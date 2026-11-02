-- 踩坑次生故障登记表（T3-12「踩坑后果传导」）。
-- 用户采纳错误建议（agree/fell）后联动注入一次次生故障（额外负载/网络劣化），
-- 本表登记"注入了什么、拿到什么实验句柄、撤销了没有"，支撑三件事：
--   1. 同一坑点只恶化一次：pitfall_message_id 唯一键幂等；
--   2. 进程重启可恢复：status 状态机（pending/injected/revoked/failed）；
--   3. 会话结束统一撤销：按 session_id 列 injected 记录，experiment_uid 供 blade destroy。
-- 执行通道（云上 fault-engine 运行时 API）不可用时记录停留在 pending/failed，仅记日志，不阻断训练。
CREATE TABLE IF NOT EXISTS ai_consequence_injections (
    id                  BIGSERIAL PRIMARY KEY,
    session_id          BIGINT      NOT NULL REFERENCES training_sessions(id) ON DELETE CASCADE,
    conversation_id     BIGINT      NOT NULL REFERENCES ai_conversations(id) ON DELETE CASCADE,
    pitfall_message_id  BIGINT      NOT NULL REFERENCES ai_messages(id) ON DELETE CASCADE,
    pitfall_config_id   BIGINT      NOT NULL,
    kind                VARCHAR(30) NOT NULL, -- cpu/memory/disk/network_delay/network_loss
    cpu_percent         INT         NOT NULL DEFAULT 0,
    mem_percent         INT         NOT NULL DEFAULT 0,
    disk_percent        INT         NOT NULL DEFAULT 0,
    delay_millis        INT         NOT NULL DEFAULT 0,
    loss_percent        INT         NOT NULL DEFAULT 0,
    duration_sec        INT         NOT NULL DEFAULT 0, -- 0=平台掌控，会话结束撤销
    status              VARCHAR(20) NOT NULL DEFAULT 'pending',
    experiment_uid      VARCHAR(100),                   -- 故障引擎实验句柄（注入成功后回填）
    fail_reason         TEXT,
    tenant_id           BIGINT      NOT NULL DEFAULT 0,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- 幂等键：一条坑点消息至多联动一次恶化。
CREATE UNIQUE INDEX IF NOT EXISTS uq_consequence_pitfall_message
    ON ai_consequence_injections (pitfall_message_id);
-- 会话结束恢复：按会话取仍 injected 的记录。
CREATE INDEX IF NOT EXISTS idx_consequence_session_status
    ON ai_consequence_injections (session_id, status);
