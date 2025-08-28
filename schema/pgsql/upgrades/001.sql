DROP TABLE object_extra_tag;

ALTER TABLE source DROP CONSTRAINT ck_source_icinga2_has_config;
ALTER TABLE source DROP CONSTRAINT IF EXISTS ck_source_bcrypt_listener_password_hash;
ALTER TABLE source ADD CONSTRAINT ck_source_bcrypt_listener_password_hash CHECK (listener_password_hash LIKE '$2y$%');
ALTER TABLE source
  DROP COLUMN icinga2_base_url,
  DROP COLUMN icinga2_auth_user,
  DROP COLUMN icinga2_auth_pass,
  Drop COLUMN icinga2_ca_pem,
  Drop COLUMN icinga2_common_name,
  Drop COLUMN icinga2_insecure_tls,
  ALTER COLUMN listener_password_hash SET NOT NULL;

ALTER TABLE rule
  ADD COLUMN source_id bigint DEFAULT NULL,
  ADD CONSTRAINT fk_rule_source FOREIGN KEY (source_id) REFERENCES source(id);

UPDATE rule SET source_id = (SELECT id FROM source WHERE type = 'icinga2');
ALTER TABLE rule ALTER COLUMN source_id SET NOT NULL;
