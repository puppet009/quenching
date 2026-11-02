-- 回滚 000007：恢复 000006 最初写入的占位 hash（对应明文 "secret"）。
-- 注意：回滚后默认账号将无法用 Admin@123 / User@123 登录，仅用于维持迁移历史一致性。

UPDATE users
SET password_hash = '$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy'
WHERE phone IN ('13800000000', '13800000001');
