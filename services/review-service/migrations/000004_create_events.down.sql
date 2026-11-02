-- 回滚：删除事件表及索引

DROP INDEX IF EXISTS idx_events_unique_user_event;
DROP INDEX IF EXISTS idx_events_occurred_at;
DROP INDEX IF EXISTS idx_events_user_event;
DROP INDEX IF EXISTS idx_events_event_type_time;

DROP TABLE IF EXISTS events;
