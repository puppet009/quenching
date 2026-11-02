-- 助手修改操作登记表（M5「修改操作登记+复盘」）。
-- 助手经用户审批后通过 write_file 改动沙箱文件，本表逐次登记
-- "改了哪个文件、写入前后内容、何时改、对应哪次审批"，支撑两件事：
--   1. 复盘可查：助手帮用户动过哪些文件，供故障排查留痕；
--   2. 前后对照：before_content 为写入前快照，before_existed 区分"新建/覆盖"。
-- 登记失败仅记日志、不使已成功的写操作报错（写操作本身不可回滚登记）。
CREATE TABLE IF NOT EXISTS ai_change_records (
    id              BIGSERIAL PRIMARY KEY,
    session_id      BIGINT      NOT NULL REFERENCES training_sessions(id) ON DELETE CASCADE,
    conversation_id BIGINT      REFERENCES ai_conversations(id) ON DELETE SET NULL,
    approval_id     VARCHAR(64),                          -- 对应用户审批句柄（观察类直改时为空）
    pod             VARCHAR(128) NOT NULL,
    container       VARCHAR(128) NOT NULL DEFAULT '',
    path            TEXT        NOT NULL,
    before_existed  BOOLEAN     NOT NULL DEFAULT false,   -- 写入前文件是否已存在
    before_content  TEXT,                                 -- 写入前内容（原不存在时为空）
    after_content   TEXT        NOT NULL,                 -- 写入后内容
    tenant_id       BIGINT      NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at      TIMESTAMPTZ
);

-- 复盘按会话取全部修改，时间正序。
CREATE INDEX IF NOT EXISTS idx_ai_change_records_session_time
    ON ai_change_records (session_id, created_at);
