-- 沙箱按活跃度续租 + 结束宽限回收所需的列。
-- last_heartbeat_at 在 000001 已建（此前无人写入），本轮作为"最后活动时间"复用：
-- 终端输入、观察类操作都会刷新它，IsExpired 以 max(created_at, last_activity) + 租期判定，
-- 活跃班次不再被创建时间硬上限踢出。
-- completed_at 表示实训结束时刻（完成/主动结束/弃班），进入结束宽限期，
-- 与 destroyed_at（namespace 实际删除时刻）区分，避免释放后重算运行时长。
ALTER TABLE sandboxes ADD COLUMN IF NOT EXISTS completed_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_sandboxes_completed_at ON sandboxes (completed_at);
