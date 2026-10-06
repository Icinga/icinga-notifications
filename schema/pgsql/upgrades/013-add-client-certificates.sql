CALL assert_correct_schema_version('v0.2.0-12');

ALTER TABLE source ADD COLUMN client_certificate_subject varchar(768) DEFAULT NULL;
ALTER TABLE source ADD CONSTRAINT uk_source_client_certificate_subject UNIQUE (client_certificate_subject);
ALTER TABLE source DROP CONSTRAINT ck_source_listener_username_or_deleted;
ALTER TABLE source ADD CONSTRAINT ck_source_listener_identity_or_deleted
    CHECK (deleted = 'y' OR listener_username IS NOT NULL OR client_certificate_subject IS NOT NULL);
ALTER TABLE source ADD CONSTRAINT ck_source_listener_cert_xor_credentials
    CHECK (listener_username IS NULL OR client_certificate_subject IS NULL);

INSERT INTO notifications_schema(version, timestamp) VALUES('v0.2.0-13', EXTRACT(EPOCH from NOW()) * 1000);
