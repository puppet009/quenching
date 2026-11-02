-- 角色与用户-角色关联（《数据库设计文档》§5.3/5.4，P0）。
-- 角色为平台级字典（user/admin/content_editor）；具体角色记录由 T1-35 种子数据写入。
CREATE TABLE IF NOT EXISTS roles (
    id           BIGSERIAL PRIMARY KEY,
    name         VARCHAR(50)  NOT NULL UNIQUE, -- user/admin/content_editor
    display_name VARCHAR(50)  NOT NULL DEFAULT '',
    description  VARCHAR(255),
    permissions  JSONB,                        -- 权限列表
    -- 全库约定列（§1.1）；平台字典 tenant_id 恒为 0。
    tenant_id    BIGINT       NOT NULL DEFAULT 0,
    deleted_at   TIMESTAMPTZ,
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS user_roles (
    id         BIGSERIAL PRIMARY KEY,
    user_id    BIGINT      NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role_id    BIGINT      NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    tenant_id  BIGINT      NOT NULL DEFAULT 0,
    deleted_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_user_roles_user_role_uniq
    ON user_roles (user_id, role_id);
