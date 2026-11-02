-- 修复：000006 写入的是网上通用的 bcrypt 占位 hash（对应明文 "secret"），
-- 导致默认账号无法用 Admin@123 / User@123 登录（返回 20002 账号或密码错误）。
-- 这里把两个种子账号的 password_hash 更新为与明文一一对应的 bcrypt(cost=10) 值。
-- 全新部署由 000006 直接写入正确 hash，本迁移对其幂等（UPDATE 成相同值）。

UPDATE users
SET password_hash = '$2a$10$WGXYT2IXlIc999krbfZzc.Dg4tht7ZKEQb2Ckw.s7ZLzZ.yyluBdC'
WHERE phone = '13800000000';

UPDATE users
SET password_hash = '$2a$10$8aKpsetkbr5sK7A6rQl6J.9IUDOHtpvARZZZwbdg2zdzdHei/GGBK'
WHERE phone = '13800000001';
