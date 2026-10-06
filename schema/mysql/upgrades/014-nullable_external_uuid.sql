CALL assert_correct_schema_version('v0.2.0-13');

ALTER TABLE channel
  MODIFY COLUMN external_uuid char(36),
  ADD CONSTRAINT ck_channel_non_deleted_needs_external_uuid CHECK (deleted = 'y' OR external_uuid IS NOT NULL);
UPDATE channel SET external_uuid = NULL WHERE deleted = 'y';

ALTER TABLE contact
  MODIFY COLUMN external_uuid char(36),
  ADD CONSTRAINT ck_contact_non_deleted_needs_external_uuid CHECK (deleted = 'y' OR external_uuid IS NOT NULL);
UPDATE contact SET external_uuid = NULL WHERE deleted = 'y';

ALTER TABLE contactgroup
  MODIFY COLUMN external_uuid char(36),
  ADD CONSTRAINT ck_contactgroup_non_deleted_needs_external_uuid CHECK (deleted = 'y' OR external_uuid IS NOT NULL);
UPDATE contactgroup SET external_uuid = NULL WHERE deleted = 'y';

INSERT INTO notifications_schema(version, timestamp) VALUES('v0.2.0-14', UNIX_TIMESTAMP() * 1000);
