-- 用户账号表：MVP 统一"账号(手机/邮箱)+密码"，不做验证码。
CREATE TABLE IF NOT EXISTS users (
    id              BIGSERIAL PRIMARY KEY,
    account_type    VARCHAR(10)  NOT NULL,              -- phone / email
    phone           VARCHAR(20)  UNIQUE,
    email           VARCHAR(100) UNIQUE,
    password_hash   VARCHAR(100) NOT NULL,              -- bcrypt
    nickname        VARCHAR(50)  NOT NULL DEFAULT '',
    avatar_url      VARCHAR(255) NOT NULL DEFAULT '',
    status          VARCHAR(20)  NOT NULL DEFAULT 'active', -- active / banned
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    -- 账号二选一：phone/email 至少有一个
    CONSTRAINT users_account_present CHECK (phone IS NOT NULL OR email IS NOT NULL)
);
CREATE INDEX IF NOT EXISTS idx_users_phone ON users(phone) WHERE phone IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_users_email ON users(email) WHERE email IS NOT NULL;
