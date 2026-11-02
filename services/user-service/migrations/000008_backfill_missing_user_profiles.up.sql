-- 历史数据兜底：早期注册的用户（如建档逻辑上线前）可能在 user_profiles 缺行，
-- 导致个人中心 LEFT JOIN 得到 collab_level=NULL、扫描进 Go string 报 500。
-- 为所有未删除、但缺少档案行的用户补建默认能力档案（collab_level 默认 C1）。
INSERT INTO user_profiles (user_id)
SELECT u.id
  FROM users u
  LEFT JOIN user_profiles p ON p.user_id = u.id
 WHERE u.deleted_at IS NULL
   AND p.user_id IS NULL
ON CONFLICT DO NOTHING;
