CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

ALTER TABLE contact ADD COLUMN external_uuid uuid CONSTRAINT uk_contact_external_uuid UNIQUE;
ALTER TABLE contactgroup ADD COLUMN external_uuid uuid CONSTRAINT uk_contactgroup_external_uuid UNIQUE;
ALTER TABLE channel ADD COLUMN external_uuid uuid CONSTRAINT uk_channel_external_uuid UNIQUE;

UPDATE contact SET external_uuid = uuid_generate_v4() WHERE external_uuid IS NULL;
UPDATE contactgroup SET external_uuid = uuid_generate_v4() WHERE external_uuid IS NULL;
UPDATE channel SET external_uuid = uuid_generate_v4() WHERE external_uuid IS NULL;

ALTER TABLE contact ALTER COLUMN external_uuid SET NOT NULL;
ALTER TABLE contactgroup ALTER COLUMN external_uuid SET NOT NULL;
ALTER TABLE channel ALTER COLUMN external_uuid SET NOT NULL;

DROP EXTENSION "uuid-ossp";

ALTER TABLE source
  DROP CONSTRAINT ck_source_icinga2_has_config,
  DROP CONSTRAINT ck_source_bcrypt_listener_password_hash;
ALTER TABLE source
  DROP COLUMN icinga2_base_url,
  DROP COLUMN icinga2_auth_user,
  DROP COLUMN icinga2_auth_pass,
  DROP COLUMN icinga2_ca_pem,
  DROP COLUMN icinga2_common_name,
  DROP COLUMN icinga2_insecure_tls,
  ADD CONSTRAINT ck_source_bcrypt_listener_password_hash CHECK (
    listener_password_hash IS NULL OR listener_password_hash LIKE '$2_$%');

ALTER TABLE rule
  ADD COLUMN source_id bigint DEFAULT NULL,
  ADD CONSTRAINT fk_rule_source FOREIGN KEY (source_id) REFERENCES source(id);

UPDATE rule SET source_id = (SELECT id FROM source WHERE type = 'icinga2');
ALTER TABLE rule ALTER COLUMN source_id SET NOT NULL;

ALTER TABLE schedule ADD COLUMN timezone text;
UPDATE schedule SET timezone = (
    SELECT entry.timezone
    FROM timeperiod_entry entry
    INNER JOIN timeperiod ON timeperiod.id = entry.timeperiod_id
    INNER JOIN rotation ON rotation.id = timeperiod.owned_by_rotation_id
    WHERE rotation.schedule_id = schedule.id
    ORDER BY entry.id
    LIMIT 1
);
UPDATE schedule SET timezone = 'UTC' WHERE timezone IS NULL;
ALTER TABLE schedule ALTER COLUMN timezone SET NOT NULL;

ALTER TABLE source ADD COLUMN listener_username varchar(255);
UPDATE source SET listener_username = CONCAT('source-', source.id) WHERE deleted = 'n';
ALTER TABLE source
    ADD CONSTRAINT uk_source_listener_username UNIQUE (listener_username),
    ADD CONSTRAINT ck_source_listener_username_or_deleted CHECK (deleted = 'y' OR listener_username IS NOT NULL);
