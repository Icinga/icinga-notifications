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
