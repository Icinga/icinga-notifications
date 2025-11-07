ALTER TABLE source ADD COLUMN listener_username varchar(255) AFTER name;
UPDATE source SET listener_username = CONCAT('source-', source.id) WHERE deleted = 'n';
ALTER TABLE source
    ADD CONSTRAINT uk_source_listener_username UNIQUE (listener_username),
    ADD CONSTRAINT ck_source_listener_username_or_deleted CHECK (deleted = 'y' OR listener_username IS NOT NULL);
