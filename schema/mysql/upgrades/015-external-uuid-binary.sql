ALTER TABLE channel
    DROP CONSTRAINT ck_channel_non_deleted_needs_external_uuid,
    DROP INDEX uk_channel_external_uuid,
    ADD COLUMN external_uuid_new binary(16) AFTER external_uuid;
UPDATE channel SET external_uuid_new = UNHEX(REPLACE(external_uuid, '-', '')) WHERE external_uuid IS NOT NULL;
ALTER TABLE channel
    DROP COLUMN external_uuid,
    CHANGE COLUMN external_uuid_new external_uuid binary(16),
    ADD CONSTRAINT uk_channel_external_uuid UNIQUE (external_uuid),
    ADD CONSTRAINT ck_channel_non_deleted_needs_external_uuid CHECK (deleted = 'y' OR external_uuid IS NOT NULL);

ALTER TABLE contact
    DROP CONSTRAINT ck_contact_non_deleted_needs_external_uuid,
    DROP INDEX uk_contact_external_uuid,
    ADD COLUMN external_uuid_new binary(16) AFTER external_uuid;
UPDATE contact SET external_uuid_new = UNHEX(REPLACE(external_uuid, '-', '')) WHERE external_uuid IS NOT NULL;
ALTER TABLE contact
    DROP COLUMN external_uuid,
    CHANGE COLUMN external_uuid_new external_uuid binary(16),
    ADD CONSTRAINT uk_contact_external_uuid UNIQUE (external_uuid),
    ADD CONSTRAINT ck_contact_non_deleted_needs_external_uuid CHECK (deleted = 'y' OR external_uuid IS NOT NULL);

ALTER TABLE contactgroup
    DROP CONSTRAINT ck_contactgroup_non_deleted_needs_external_uuid,
    DROP INDEX uk_contactgroup_external_uuid,
    ADD COLUMN external_uuid_new binary(16) AFTER external_uuid;
UPDATE contactgroup SET external_uuid_new = UNHEX(REPLACE(external_uuid, '-', '')) WHERE external_uuid IS NOT NULL;
ALTER TABLE contactgroup
    DROP COLUMN external_uuid,
    CHANGE COLUMN external_uuid_new external_uuid binary(16),
    ADD CONSTRAINT uk_contactgroup_external_uuid UNIQUE (external_uuid),
    ADD CONSTRAINT ck_contactgroup_non_deleted_needs_external_uuid CHECK (deleted = 'y' OR external_uuid IS NOT NULL);
