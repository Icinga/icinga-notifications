ALTER TABLE source
    ALTER COLUMN listener_password_hash DROP NOT NULL,

    ADD COLUMN icinga2_base_url text,
    ADD COLUMN icinga2_auth_user text,
    ADD COLUMN icinga2_auth_pass text,
    ADD COLUMN icinga2_ca_pem text,
    ADD COLUMN icinga2_insecure_tls boolenum NOT NULL DEFAULT 'n',

    DROP CONSTRAINT source_listener_password_hash_check;

-- NOTE: Change those defaults as they most likely don't work with your installation!
UPDATE source
    SET icinga2_base_url = 'https://localhost:5665/',
        icinga2_auth_user = 'root',
        icinga2_auth_pass = 'icinga',
        icinga2_insecure_tls = 'y'
    WHERE type = 'icinga2';

ALTER TABLE source
    ADD CHECK (listener_password_hash IS NULL OR listener_password_hash LIKE '$2y$%'),
    ADD CHECK (type != 'icinga2' OR (icinga2_base_url IS NOT NULL AND icinga2_auth_user IS NOT NULL AND icinga2_auth_pass IS NOT NULL));
