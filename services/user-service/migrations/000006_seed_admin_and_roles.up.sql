-- 种子数据：管理员账号、角色、示例用户（T1-35）
-- 包含：
--   1. 角色字典（user / admin / content_editor）
--   2. 超级管理员账号（admin / Admin@123）
--   3. 演示用户账号（demo / User@123）
--
-- 注意：
--   - 密码 hash 为 bcrypt cost=10，与明文一一对应（Admin@123 / User@123）
--   - 行为模拟剧本种子数据在 sandbox-service 的 000003_seed_behavior_scenarios.up.sql

-- ===== 1. 角色字典 =====
INSERT INTO roles (name, display_name, description, permissions, tenant_id) VALUES
(
    'user',
    '普通用户',
    '普通训练用户，可参加训练、查看自己的历史记录',
    '["training:join","training:view","review:view","profile:edit"]',
    0
),
(
    'admin',
    '超级管理员',
    '平台超级管理员，拥有全部权限',
    '["*"]',
    0
),
(
    'content_editor',
    '内容运营',
    '内容运营人员，可管理项目、故障剧本、坑点配置等内容',
    '["project:manage","defect:manage","pitfall:manage","content:view"]',
    0
)
ON CONFLICT (name) DO NOTHING;

-- ===== 2. 超级管理员账号 =====
-- 账号：13800000000 / admin@quenching.dev
-- 密码：Admin@123（bcrypt cost=10）
INSERT INTO users (account_type, phone, email, password_hash, nickname, status) VALUES
(
    'phone',
    '13800000000',
    'admin@quenching.dev',
    '$2a$10$WGXYT2IXlIc999krbfZzc.Dg4tht7ZKEQb2Ckw.s7ZLzZ.yyluBdC',
    '超级管理员',
    'active'
)
ON CONFLICT (phone) DO NOTHING;

-- 分配 admin 角色
INSERT INTO user_roles (user_id, role_id, tenant_id)
SELECT u.id, r.id, 0
FROM users u
CROSS JOIN roles r
WHERE u.phone = '13800000000'
  AND r.name = 'admin'
ON CONFLICT (user_id, role_id) DO NOTHING;

-- ===== 3. 演示用户 =====
-- 账号：13800000001 / demo@quenching.dev
-- 密码：User@123（bcrypt cost=10）
INSERT INTO users (account_type, phone, email, password_hash, nickname, status) VALUES
(
    'phone',
    '13800000001',
    'demo@quenching.dev',
    '$2a$10$8aKpsetkbr5sK7A6rQl6J.9IUDOHtpvARZZZwbdg2zdzdHei/GGBK',
    '演示用户',
    'active'
)
ON CONFLICT (phone) DO NOTHING;

INSERT INTO user_roles (user_id, role_id, tenant_id)
SELECT u.id, r.id, 0
FROM users u
CROSS JOIN roles r
WHERE u.phone = '13800000001'
  AND r.name = 'user'
ON CONFLICT (user_id, role_id) DO NOTHING;
