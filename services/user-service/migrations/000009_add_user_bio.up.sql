-- 基本资料编辑：个人简介落在 users 表（与 nickname/avatar_url 同级的账号展示信息）。
ALTER TABLE users ADD COLUMN IF NOT EXISTS bio VARCHAR(200) NOT NULL DEFAULT '';
