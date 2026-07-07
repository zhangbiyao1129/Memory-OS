-- 本机 MCP 加解密改造：服务端只存密文和元信息，不再持有解密材料。
-- secrets 增加用途/环境/站点/过期元信息；secret_versions 增加算法与设备 key 指纹。

ALTER TABLE secrets ADD COLUMN IF NOT EXISTS env_name TEXT;
ALTER TABLE secrets ADD COLUMN IF NOT EXISTS site TEXT;
ALTER TABLE secrets ADD COLUMN IF NOT EXISTS purpose TEXT;
ALTER TABLE secrets ADD COLUMN IF NOT EXISTS expires_at TIMESTAMPTZ;

ALTER TABLE secret_versions ADD COLUMN IF NOT EXISTS algorithm TEXT NOT NULL DEFAULT 'AES-256-GCM';
ALTER TABLE secret_versions ADD COLUMN IF NOT EXISTS device_key_id TEXT NOT NULL DEFAULT '';
ALTER TABLE secret_versions ADD COLUMN IF NOT EXISTS key_fingerprint TEXT NOT NULL DEFAULT '';
