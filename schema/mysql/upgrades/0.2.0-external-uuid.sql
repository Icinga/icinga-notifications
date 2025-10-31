ALTER TABLE contact ADD COLUMN external_uuid char(36) AFTER id;
ALTER TABLE contactgroup ADD COLUMN external_uuid char(36) AFTER id;
ALTER TABLE channel ADD COLUMN external_uuid char(36) AFTER id;

UPDATE contact SET external_uuid = UUID() WHERE external_uuid IS NULL;
UPDATE contactgroup SET external_uuid = UUID() WHERE external_uuid IS NULL;
UPDATE channel SET external_uuid = UUID() WHERE external_uuid IS NULL;

ALTER TABLE contact MODIFY COLUMN external_uuid char(36) NOT NULL;
ALTER TABLE contactgroup MODIFY COLUMN external_uuid char(36) NOT NULL;
ALTER TABLE channel MODIFY COLUMN external_uuid char(36) NOT NULL;

ALTER TABLE contact ADD CONSTRAINT uk_contact_external_uuid UNIQUE (external_uuid);
ALTER TABLE contactgroup ADD CONSTRAINT uk_contactgroup_external_uuid UNIQUE (external_uuid);
ALTER TABLE channel ADD CONSTRAINT uk_channel_external_uuid UNIQUE (external_uuid);
