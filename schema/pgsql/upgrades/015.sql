TRUNCATE TABLE object CASCADE;

CREATE TYPE incident_history_event_type_new AS ENUM ( 'incident_severity_changed', 'recipient_role_changed', 'escalation_triggered', 'rule_matched', 'opened', 'closed', 'notified' );
ALTER TABLE incident_history
  ALTER COLUMN type TYPE incident_history_event_type_new USING type::text::incident_history_event_type_new;

DROP TYPE incident_history_event_type;
ALTER TYPE incident_history_event_type_new RENAME TO incident_history_event_type;

ALTER TABLE object
  ADD COLUMN source_id bigint NOT NULL REFERENCES source(id),
  ADD COLUMN name text NOT NULL,
  ADD COLUMN url text;

ALTER TABLE event
  DROP CONSTRAINT event_source_id_fkey,
  DROP CONSTRAINT event_object_id_source_id_fkey,
  DROP COLUMN source_id;

ALTER TABLE object_extra_tag
  DROP CONSTRAINT object_extra_tag_object_id_source_id_fkey,
  DROP CONSTRAINT object_extra_tag_source_id_fkey,
  DROP COLUMN source_id,
  ADD CONSTRAINT pk_object_extra_tag PRIMARY KEY (object_id, tag);

DROP TABLE source_object;
