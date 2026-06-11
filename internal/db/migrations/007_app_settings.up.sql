-- 运行时应用设置（键值），目前承载 allow_registration
CREATE TABLE app_settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
