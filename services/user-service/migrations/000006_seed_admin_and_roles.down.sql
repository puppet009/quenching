-- 回滚：删除种子数据

-- 删除用户角色关联
DELETE FROM user_roles
WHERE user_id IN (SELECT id FROM users WHERE phone IN ('13800000000', '13800000001'));

-- 删除种子用户
DELETE FROM users WHERE phone IN ('13800000000', '13800000001');

-- 删除角色
DELETE FROM roles WHERE name IN ('user', 'admin', 'content_editor');
