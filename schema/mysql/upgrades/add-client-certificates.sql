ALTER TABLE source ADD COLUMN client_certificate_cn varchar(64) DEFAULT NULL AFTER listener_password_hash;
ALTER TABLE source ADD CONSTRAINT uk_source_client_certificate_cn UNIQUE (client_certificate_cn);
ALTER TABLE source DROP CONSTRAINT ck_source_listener_username_or_deleted;
ALTER TABLE source ADD CONSTRAINT ck_source_listener_identity_or_deleted
    CHECK (deleted = 'y' OR listener_username IS NOT NULL OR client_certificate_cn IS NOT NULL);