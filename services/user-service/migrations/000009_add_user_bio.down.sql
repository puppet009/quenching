-- 回滚 000009：删除个人简介列（已填写内容会丢失，回滚前请确认）。
ALTER TABLE users DROP COLUMN IF EXISTS bio;
