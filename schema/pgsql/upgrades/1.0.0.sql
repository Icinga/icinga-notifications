-- schema: prefix upgrade scripts with an incrementing number

DROP TABLE object_extra_tag;

-- schema: prefix upgrade scripts with an incrementing number

ALTER TABLE source ADD COLUMN locked boolenum NOT NULL DEFAULT 'n';

-- schema: assert and stamp schema versions in development upgrades

-- This procedure can be used in upgrade scripts to assert that the schema version in the database matches the
-- expected version before applying the upgrade. This is important to prevent users from accidentally skipping
-- intermediate upgrade scripts, which could lead to an inconsistent database state. For instance, since every
-- upgrade script knows its predecessor's version, we can just do "CALL assert_correct_schema_version('v1.0')"
-- at the beginning of the 1.x upgrade scripts to ensure that the 1.0 script has been applied before.
CREATE OR REPLACE PROCEDURE assert_correct_schema_version(expected_version text)
    LANGUAGE plpgsql
AS $$
DECLARE
    actual_version text := (SELECT version FROM notifications_schema ORDER BY id DESC LIMIT 1);
BEGIN
    IF actual_version IS NULL THEN
        RAISE 'Schema version not found in notifications_schema table.';
    ELSIF actual_version != expected_version THEN
        RAISE 'Schema version mismatch: expected %, got %. Please apply all previous upgrade scripts in order before applying this one.', expected_version, actual_version;
    END IF;
END;
$$;
COMMENT ON PROCEDURE assert_correct_schema_version IS 'Asserts that the schema version in the database matches the expected version and raises an error if not.';

CREATE TABLE notifications_schema (
  id serial,
  version varchar(64) NOT NULL,
  timestamp bigint NOT NULL,

  CONSTRAINT pk_notifications_schema PRIMARY KEY (id),
  CONSTRAINT uk_notifications_schema_version UNIQUE (version)
);

INSERT INTO notifications_schema(version, timestamp) VALUES('v0.2.0-3', EXTRACT(EPOCH from NOW()) * 1000);

-- schema: assert and stamp schema versions in development upgrades

CALL assert_correct_schema_version('v0.2.0-3');

DROP TABLE incident_event;
ALTER TABLE incident_history DROP CONSTRAINT fk_incident_history_event;
ALTER TABLE incident_history DROP COLUMN event_id;
DROP TABLE event;
DROP TYPE IF EXISTS event_type;

INSERT INTO notifications_schema(version, timestamp) VALUES('v0.2.0-4', EXTRACT(EPOCH from NOW()) * 1000);

-- schema: assert and stamp schema versions in development upgrades

CALL assert_correct_schema_version('v0.2.0-4');

ALTER TABLE incident ADD COLUMN mute_reason text DEFAULT NULL;

-- Migrate currently active muted state from object onto its open incident.
UPDATE incident
  SET mute_reason = object.mute_reason
  FROM object
  WHERE object.id = incident.object_id
    AND incident.recovered_at IS NULL
    AND object.mute_reason IS NOT NULL;

ALTER TABLE object DROP COLUMN mute_reason;

INSERT INTO notifications_schema(version, timestamp) VALUES('v0.2.0-5', EXTRACT(EPOCH from NOW()) * 1000);

-- schema: assert and stamp schema versions in development upgrades

CALL assert_correct_schema_version('v0.2.0-5');

ALTER TABLE incident_contact ADD COLUMN changed_at bigint;
UPDATE incident_contact SET changed_at = ih.time
  FROM incident_history ih WHERE (
    ih.incident_id,
    COALESCE(ih.contact_id, 0),
    COALESCE(ih.contactgroup_id, 0),
    COALESCE(ih.schedule_id, 0)
  ) = (
    incident_contact.incident_id,
    COALESCE(incident_contact.contact_id, 0),
    COALESCE(incident_contact.contactgroup_id, 0),
    COALESCE(incident_contact.schedule_id, 0)
  );
UPDATE incident_contact SET changed_at = EXTRACT(EPOCH from NOW()) * 1000 WHERE changed_at IS NULL;
ALTER TABLE incident_contact ALTER COLUMN changed_at SET NOT NULL;

INSERT INTO notifications_schema(version, timestamp) VALUES('v0.2.0-6', EXTRACT(EPOCH from NOW()) * 1000);

-- schema: assert and stamp schema versions in development upgrades

CALL assert_correct_schema_version('v0.2.0-6');

ALTER TABLE incident ADD COLUMN message text;

INSERT INTO notifications_schema(version, timestamp) VALUES('v0.2.0-7', EXTRACT(EPOCH from NOW()) * 1000);

-- schema: assert and stamp schema versions in development upgrades

CALL assert_correct_schema_version('v0.2.0-7');

CREATE INDEX idx_incident_recovered_at ON incident(recovered_at);

INSERT INTO notifications_schema(version, timestamp) VALUES('v0.2.0-8', EXTRACT(EPOCH from NOW()) * 1000);

-- schema: assert and stamp schema versions in development upgrades

CALL assert_correct_schema_version('v0.2.0-8');

CREATE TABLE event_queue (
    id bytea NOT NULL, -- SHA256 of JSON representation.

    json text NOT NULL,
    time bigint NOT NULL,
    object_id bytea NOT NULL, -- No foreign key, object might not exist at this point.

    user_agent varchar(255) NOT NULL, -- From submitting client; allows migrations after upgrades.
    state smallint NOT NULL DEFAULT 0, -- pending (0), processing (1), done (2), or error (64).

    CONSTRAINT pk_event_queue PRIMARY KEY (id)
);

CREATE INDEX idx_event_queue_time ON event_queue (time);
CREATE INDEX idx_event_queue_time_state ON event_queue (time, state);
CREATE INDEX idx_event_queue_state_object_id ON event_queue (state, object_id);

INSERT INTO notifications_schema(version, timestamp) VALUES('v0.2.0-9', EXTRACT(EPOCH from NOW()) * 1000);

-- schema: assert and stamp schema versions in development upgrades

CALL assert_correct_schema_version('v0.2.0-9');

-- Upgrade completes ha-init.sql.

DROP INDEX idx_event_queue_time;
DROP INDEX idx_event_queue_time_state;

ALTER TABLE event_queue
	RENAME COLUMN time TO last_update;

ALTER TABLE event_queue
	ADD COLUMN event_time bigint NULL;

UPDATE event_queue SET event_time = last_update;

ALTER TABLE event_queue
	ALTER COLUMN event_time SET NOT NULL;

CREATE INDEX idx_event_queue_last_update ON event_queue (last_update);
CREATE INDEX idx_event_queue_last_update_state ON event_queue (last_update, state);

INSERT INTO notifications_schema(version, timestamp) VALUES('v0.2.0-10', EXTRACT(EPOCH from NOW()) * 1000);

-- schema: assert and stamp schema versions in development upgrades

CALL assert_correct_schema_version('v0.2.0-10');

ALTER TABLE incident_contact
  ADD COLUMN id bigserial,
  ADD CONSTRAINT pk_incident_contact PRIMARY KEY (id);

INSERT INTO notifications_schema(version, timestamp) VALUES('v0.2.0-11', EXTRACT(EPOCH from NOW()) * 1000);

-- schema: assert and stamp schema versions in development upgrades

CALL assert_correct_schema_version('v0.2.0-11');

ALTER TABLE incident ADD COLUMN next_escalation_check_at bigint;
CREATE INDEX idx_incident_object_id_recovered_at ON incident(object_id, recovered_at);
CREATE INDEX idx_incident_next_escalation_check_at ON incident(next_escalation_check_at);
CREATE INDEX idx_incident_recovered_at_next_escalation_check_at ON incident(recovered_at, next_escalation_check_at);

INSERT INTO notifications_schema(version, timestamp) VALUES('v0.2.0-12', EXTRACT(EPOCH from NOW()) * 1000);

-- schema: assert and stamp schema versions in development upgrades

CALL assert_correct_schema_version('v0.2.0-12');

ALTER TABLE source ADD COLUMN client_certificate_subject varchar(768) DEFAULT NULL;
ALTER TABLE source ADD CONSTRAINT uk_source_client_certificate_subject UNIQUE (client_certificate_subject);
ALTER TABLE source DROP CONSTRAINT ck_source_listener_username_or_deleted;
ALTER TABLE source ADD CONSTRAINT ck_source_listener_identity_or_deleted
    CHECK (deleted = 'y' OR listener_username IS NOT NULL OR client_certificate_subject IS NOT NULL);
ALTER TABLE source ADD CONSTRAINT ck_source_listener_cert_xor_credentials
    CHECK (listener_username IS NULL OR client_certificate_subject IS NULL);

INSERT INTO notifications_schema(version, timestamp) VALUES('v0.2.0-13', EXTRACT(EPOCH from NOW()) * 1000);

-- schema: assert and stamp schema versions in development upgrades

CALL assert_correct_schema_version('v0.2.0-13');

CREATE INDEX idx_object_id_tag_object_id ON object_id_tag(object_id);
CREATE INDEX idx_incident_object_id ON incident(object_id);
CREATE INDEX idx_incident_contact_incident_id ON incident_contact(incident_id);
CREATE INDEX idx_incident_rule_incident_id ON incident_rule(incident_id);
CREATE INDEX idx_incident_rule_escalation_state_incident_id ON incident_rule_escalation_state(incident_id);
CREATE INDEX idx_incident_history_incident_id ON incident_history(incident_id);

INSERT INTO notifications_schema(version, timestamp) VALUES('v0.2.0-14', EXTRACT(EPOCH from NOW()) * 1000);

-- schema: assert and stamp schema versions in development upgrades

CALL assert_correct_schema_version('v0.2.0-14');

ALTER TABLE channel
  ALTER COLUMN external_uuid DROP NOT NULL,
  ADD CONSTRAINT ck_channel_non_deleted_needs_external_uuid CHECK (deleted = 'y' OR external_uuid IS NOT NULL);
UPDATE channel SET external_uuid = NULL WHERE deleted = 'y';

ALTER TABLE contact
  ALTER COLUMN external_uuid DROP NOT NULL,
  ADD CONSTRAINT ck_contact_non_deleted_needs_external_uuid CHECK (deleted = 'y' OR external_uuid IS NOT NULL);
UPDATE contact SET external_uuid = NULL WHERE deleted = 'y';

ALTER TABLE contactgroup
  ALTER COLUMN external_uuid DROP NOT NULL,
  ADD CONSTRAINT ck_contactgroup_non_deleted_needs_external_uuid CHECK (deleted = 'y' OR external_uuid IS NOT NULL);
UPDATE contactgroup SET external_uuid = NULL WHERE deleted = 'y';

INSERT INTO notifications_schema(version, timestamp) VALUES('v0.2.0-15', EXTRACT(EPOCH from NOW()) * 1000);

-- schema: assert and stamp schema versions in development upgrades

CALL assert_correct_schema_version('v0.2.0-15');

CREATE TABLE channel_state (
    channel_id bigint NOT NULL,
    state_key varchar(255) NOT NULL,
    value varchar(4096) NOT NULL,

    CONSTRAINT pk_channel_state PRIMARY KEY (channel_id, state_key),
    CONSTRAINT fk_channel_state_channel FOREIGN KEY (channel_id) REFERENCES channel(id)
);
CREATE INDEX idx_channel_state_channel_id ON channel_state(channel_id);

INSERT INTO notifications_schema(version, timestamp) VALUES('v0.2.0-16', EXTRACT(EPOCH from NOW()) * 1000);

-- schema: assert and stamp schema versions in development upgrades

CALL assert_correct_schema_version('v0.2.0-16');

CREATE TABLE object_source (
    object_id bytea NOT NULL,
    source_id bigint NOT NULL, -- The specific source that the object referenced by object_id is associated with.

    CONSTRAINT pk_object_source PRIMARY KEY (object_id, source_id),
    CONSTRAINT fk_object_source_object FOREIGN KEY (object_id) REFERENCES object(id),
    CONSTRAINT fk_object_source_source FOREIGN KEY (source_id) REFERENCES source(id)
);
-- Indexes for object_source to speed up the cleanup queries by object id.
CREATE INDEX idx_object_source_object_id ON object_source(object_id);

-- The object ID is going to be re-generated, so we need to clear the table and reset the sequence in a cascade manner.
TRUNCATE TABLE object RESTART IDENTITY CASCADE;
ALTER TABLE object
  DROP CONSTRAINT fk_object_source,
  DROP COLUMN source_id;

ALTER TABLE rule ADD COLUMN source_type text;
UPDATE rule SET source_type = source.type FROM source WHERE rule.source_id = source.id;
ALTER TABLE rule
  DROP CONSTRAINT fk_rule_source,
  DROP COLUMN source_id,
  ALTER COLUMN source_type SET NOT NULL;

INSERT INTO notifications_schema(version, timestamp) VALUES('v0.2.0-17', EXTRACT(EPOCH from NOW()) * 1000);

-- schema: assert and stamp schema versions in development upgrades

CALL assert_correct_schema_version('v0.2.0-17');

DROP TABLE event_queue;
CREATE TABLE job_queue (
    id bytea NOT NULL, -- SHA256 of JSON representation of the envelope.
    last_update bigint NOT NULL,
    state smallint NOT NULL DEFAULT 0, -- pending (0), processing (1), done (2), or error (64).
    envelope text NOT NULL,

    CONSTRAINT pk_job_queue PRIMARY KEY (id)
);

-- These indices are used to speed up the cleanup queries by last_update and state.
CREATE INDEX idx_job_queue_last_update ON job_queue (last_update);
CREATE INDEX idx_job_queue_last_update_state ON job_queue (last_update, state);

CREATE TABLE job_processing_lock (
    object_id bytea NOT NULL, -- No foreign key, object might not exist at this point.
    job_queue_id bytea NOT NULL, -- references job_queue.id

    CONSTRAINT pk_job_processing_lock PRIMARY KEY (object_id),
    CONSTRAINT fk_job_processing_lock_job_queue FOREIGN KEY (job_queue_id) REFERENCES job_queue(id),
    CONSTRAINT uk_job_processing_lock_job_queue_id UNIQUE (job_queue_id)
);

INSERT INTO notifications_schema(version, timestamp) VALUES('v0.2.0-18', EXTRACT(EPOCH from NOW()) * 1000);

-- schema: assert and stamp schema versions in development upgrades

CALL assert_correct_schema_version('v0.2.0-18');

CREATE TYPE notification_history_state_type AS ENUM ( 'sent', 'failed' );

ALTER TABLE incident_history ADD COLUMN event_id uuid; -- used for external references
-- This will be used by the query in the [Incident.RetriggerEscalations] method in icinga-notifications.
CREATE INDEX idx_incident_history_event_id_incident_id ON incident_history(event_id, incident_id);

TRUNCATE TABLE job_queue CASCADE;

ALTER TABLE job_processing_lock DROP COLUMN job_queue_id;
ALTER TABLE job_queue
  DROP COLUMN id,
  ADD COLUMN id uuid NOT NULL,
  ADD CONSTRAINT pk_job_queue PRIMARY KEY (id);

ALTER TABLE job_processing_lock
  ADD COLUMN job_queue_id uuid NOT NULL,
  ADD CONSTRAINT fk_job_processing_lock_job_queue FOREIGN KEY (job_queue_id) REFERENCES job_queue(id),
  ADD CONSTRAINT uk_job_processing_lock_job_queue_id UNIQUE (job_queue_id);

CREATE TABLE notification_history (
    id bigserial,
    object_id bytea NOT NULL,
    event_id uuid NOT NULL,
    contact_id bigint NOT NULL,
    contactgroup_id bigint,
    schedule_id bigint,
    channel_id bigint NOT NULL,
    incident_id bigint,
    event_message text NOT NULL,
    state notification_history_state_type NOT NULL,
    triggered_at bigint NOT NULL,

    CONSTRAINT pk_notification_history PRIMARY KEY (id),
    CONSTRAINT fk_notification_history_object_id FOREIGN KEY (object_id) REFERENCES object(id)
);

CREATE INDEX idx_notification_history_triggered_at ON notification_history(triggered_at);
CREATE INDEX idx_notification_history_state_triggered_at ON notification_history(state, triggered_at);
-- This index is required for the "object" retention query, which becomes quite unusable without it.
CREATE INDEX idx_notification_history_object_id ON notification_history(object_id);

CREATE TABLE skipped_notification_history (
    id bigserial,
    notification_history_id bigint NOT NULL, -- The actual notification due to which the notification was skipped.
    rule_id bigint NOT NULL,
    rule_escalation_id bigint NOT NULL,
    contactgroup_id bigint,
    schedule_id bigint,

    CONSTRAINT pk_skipped_notification_history PRIMARY KEY (id),
    CONSTRAINT fk_skipped_notification_history_notification_history FOREIGN KEY (notification_history_id) REFERENCES notification_history(id)
);

-- This index is required for the "notification history" retention query to identify and delete related references.
CREATE INDEX idx_skipped_notification_history_notification_history_id ON skipped_notification_history(notification_history_id);

INSERT INTO notifications_schema(version, timestamp) VALUES('v0.2.0-19', EXTRACT(EPOCH from NOW()) * 1000);

-- schema: assert and stamp schema versions in development upgrades

CALL assert_correct_schema_version('v0.2.0-19');

-- A procedure cannot be replaced by a function of the same name, so it must be dropped first.
DROP PROCEDURE assert_correct_schema_version(text);
CREATE FUNCTION assert_correct_schema_version(expected_version text)
    RETURNS void
    LANGUAGE plpgsql
    STABLE
    STRICT
    PARALLEL RESTRICTED
AS $$
DECLARE
    actual_version text;
BEGIN
    SELECT version INTO actual_version FROM notifications_schema ORDER BY id DESC LIMIT 1;

    IF actual_version IS NULL THEN
        RAISE 'Schema version not found in notifications_schema table.';
    ELSIF actual_version != expected_version THEN
        RAISE 'Schema version mismatch: expected %, got %. Please apply all previous upgrade scripts in order before applying this one.', expected_version, actual_version;
    END IF;
END;
$$;

INSERT INTO notifications_schema(version, timestamp) VALUES('v0.2.0-20', EXTRACT(EPOCH from NOW()) * 1000);

-- schema: assert and stamp schema versions in development upgrades

SELECT assert_correct_schema_version('v0.2.0-20');

ALTER TABLE channel
  ADD COLUMN validation_result text;

INSERT INTO notifications_schema(version, timestamp) VALUES('v0.2.0-21', EXTRACT(EPOCH from NOW()) * 1000);

-- schema: assert and stamp schema versions in development upgrades

SELECT assert_correct_schema_version('v0.2.0-21');

-- This script is used to upgrade the escalation condition column in the rule_escalation table from a raw filter
-- string to the new JSON object format. The raw filter string is a simple string that represents a logical condition,
-- while the new JSON object format is a structured representation of the same condition. For filter chains, this
-- script assumes that the logical operator is always &, so if somehow a different logical op is used, the resulting
-- JSON object will be invalid.
CREATE OR REPLACE FUNCTION upgrade_escalation_condition_to_json_object()
    RETURNS void
    LANGUAGE plpgsql
    STRICT
    PARALLEL RESTRICTED
AS $$
DECLARE
    rec RECORD;
    parts text[]; -- Array to hold the parts of the condition (attribute, operator, value)
    cond text; -- Variable to hold each individual condition when splitting by &
    json_rules jsonb; -- JSON array to hold the converted rules
BEGIN
    <<records_loop>>
    FOR rec IN (SELECT id, condition FROM rule_escalation WHERE condition IS NOT NULL) LOOP
        json_rules := '[]'::jsonb;

        -- If the raw filter string doesn't contain the logical operator &, we only have a single condition,
        -- so we need to directly convert it to a JSON literal similar to this:
        -- {...,"ast":{"op":"!=","attributes":["is_managed"],"value":"n"}}
        IF position('&' IN rec.condition) = 0 THEN
            parts := regexp_matches(rec.condition, '([^!=><]+)(!=|=|>|<|>=|<=)(.*)');
            -- If the extracted parts array doesn't have exactly 3 elements, we have an invalid condition, so skip this record.
            IF array_length(parts, 1) <> 3 THEN
                CONTINUE records_loop; -- We have an invalid condition, so skip this record.
            END IF;
            UPDATE rule_escalation SET condition = jsonb_build_object(
                'version', 2,
                'qs', rec.condition,
                'ast', jsonb_build_object(
                    'op', parts[2],
                    'attributes', jsonb_build_array(parts[1]),
                    'value', parts[3]
                )
            )::text
            WHERE id = rec.id;
        ELSE
            -- Otherwise, we have a & filter chain, so we need to split the raw filter string into individual
            -- conditions and convert each one to a final JSON object similar to this:
            -- {...,"ast":{"op":"&","rules":[{"op":">=","attributes":["incident_age"],"value":"1h"},...]}}
            FOREACH cond IN ARRAY string_to_array(rec.condition, '&') LOOP
                parts := regexp_matches(cond, '([^!=><]+)(!=|=|>|<|>=|<=)(.*)');
                IF array_length(parts, 1) != 3 THEN
                    CONTINUE records_loop; -- We have an invalid condition, so skip this record.
                END IF;
                json_rules := json_rules || jsonb_build_object(
                    'op', parts[2],
                    'attributes', jsonb_build_array(parts[1]),
                    'value', parts[3]
                );
            END LOOP;

            -- Finally, construct the final JSON object with the logical operator & and the array of converted rules,
            -- and update the corresponding tuple in the table with the resulting JSON object.
            UPDATE rule_escalation SET condition = jsonb_build_object(
                'version', 2,
                'qs', rec.condition,
                'ast', jsonb_build_object('op', '&', 'rules', json_rules)
            )::text
            WHERE id = rec.id;
        END IF;
    END LOOP;
END;
$$;

-- Run the upgrade and then drop the routine immediately as it is no longer needed after the upgrade is complete.
SELECT upgrade_escalation_condition_to_json_object();
DROP FUNCTION upgrade_escalation_condition_to_json_object();

INSERT INTO notifications_schema(version, timestamp) VALUES('v0.2.0-22', EXTRACT(EPOCH from NOW()) * 1000);

-- schema: assert and stamp schema versions in development upgrades

-- Redefine assert_correct_schema_version() as it determined the latest schema version by timestamp, which isn't
-- unique if upgrades are applied in quick succession.
DROP ROUTINE IF EXISTS assert_correct_schema_version(text);
CREATE FUNCTION assert_correct_schema_version(expected_version text)
    RETURNS void
    LANGUAGE plpgsql
    STABLE
    STRICT
    PARALLEL RESTRICTED
AS $$
DECLARE
    actual_version text;
BEGIN
    SELECT version INTO actual_version FROM notifications_schema ORDER BY id DESC LIMIT 1;

    IF actual_version IS NULL THEN
        RAISE 'Schema version not found in notifications_schema table.';
    ELSIF actual_version != expected_version THEN
        RAISE 'Schema version mismatch: expected %, got %. Please apply all previous upgrade scripts in order before applying this one.', expected_version, actual_version;
    END IF;
END;
$$;

-- Until the upgrade scripts stamped their own schema version, 003-schema-version-table.sql stamped v1.0. Databases
-- upgraded by then are assumed to have applied all upgrades preceding this one, so their version is corrected first.
UPDATE notifications_schema SET version = 'v0.2.0-22' WHERE version = 'v1.0';

SELECT assert_correct_schema_version('v0.2.0-22');

INSERT INTO notifications_schema(version, timestamp) VALUES('v0.2.0-23', EXTRACT(EPOCH from NOW()) * 1000);

-- notifications: include `incident_closed` in `/notification-history` response

SELECT assert_correct_schema_version('v0.2.0-23');

ALTER TABLE notification_history ADD COLUMN incident_closed boolenum NOT NULL DEFAULT 'n';

INSERT INTO notifications_schema(version, timestamp) VALUES('v0.2.0-24', EXTRACT(EPOCH from NOW()) * 1000);

-- Add rule type and per-subscriber event types

SELECT assert_correct_schema_version('v0.2.0-24');

CREATE TYPE rule_type AS ENUM ('notification', 'escalation');
ALTER TABLE rule ADD COLUMN type rule_type NOT NULL DEFAULT 'escalation';
ALTER TABLE rule ALTER COLUMN type DROP DEFAULT;

ALTER TABLE incident_contact ADD COLUMN event_types text;

-- Drop the not null clauses before renaming the table, otherwise PostgreSQL will automatically
-- create a non_null constraint with the old name.
ALTER TABLE rule_escalation
    DROP CONSTRAINT pk_rule_escalation CASCADE, -- CASCADE to all child tables.
    ALTER COLUMN id DROP NOT NULL,
    ALTER COLUMN rule_id DROP NOT NULL,
    ALTER COLUMN changed_at DROP NOT NULL,
    ALTER COLUMN deleted DROP NOT NULL;

ALTER TABLE rule_escalation RENAME TO rule_entry;
ALTER SEQUENCE rule_escalation_id_seq RENAME TO rule_entry_id_seq;
ALTER INDEX idx_rule_escalation_changed_at RENAME TO idx_rule_entry_changed_at;
ALTER TABLE rule_entry RENAME CONSTRAINT uk_rule_escalation_rule_id_position TO uk_rule_entry_rule_id_position;
ALTER TABLE rule_entry RENAME CONSTRAINT ck_rule_escalation_not_both_condition_and_fallback_for TO ck_rule_entry_not_both_condition_and_fallback_for;
ALTER TABLE rule_entry RENAME CONSTRAINT ck_rule_escalation_non_deleted_needs_position TO ck_rule_entry_non_deleted_needs_position;
ALTER TABLE rule_entry RENAME CONSTRAINT fk_rule_escalation_rule TO fk_rule_entry_rule;

-- Now, undo the not null clauses on the renamed table to restore the original constraints.
ALTER TABLE rule_entry
    ALTER COLUMN id SET NOT NULL,
    ALTER COLUMN rule_id SET NOT NULL,
    ALTER COLUMN changed_at SET NOT NULL,
    ALTER COLUMN deleted SET NOT NULL,
    ADD CONSTRAINT pk_rule_entry PRIMARY KEY (id),
    ADD CONSTRAINT fk_rule_entry_rule_entry FOREIGN KEY (fallback_for) REFERENCES rule_entry(id);

-- Similarly, drop the not null clauses before renaming the table, otherwise PostgreSQL will
-- automatically create a non_null constraint with the old name.
ALTER TABLE rule_escalation_recipient
    DROP CONSTRAINT pk_rule_escalation_recipient,
    ALTER COLUMN id DROP NOT NULL,
    ALTER COLUMN rule_escalation_id DROP NOT NULL,
    ALTER COLUMN changed_at DROP NOT NULL,
    ALTER COLUMN deleted DROP NOT NULL;

ALTER TABLE rule_escalation_recipient RENAME TO rule_entry_recipient;
ALTER SEQUENCE rule_escalation_recipient_id_seq RENAME TO rule_entry_recipient_id_seq;
ALTER INDEX idx_rule_escalation_recipient_changed_at RENAME TO idx_rule_entry_recipient_changed_at;
ALTER TABLE rule_entry_recipient RENAME COLUMN rule_escalation_id TO rule_entry_id;
ALTER TABLE rule_entry_recipient RENAME CONSTRAINT ck_rule_escalation_recipient_has_exactly_one_recipient TO ck_rule_entry_recipient_has_exactly_one_recipient;
ALTER TABLE rule_entry_recipient RENAME CONSTRAINT fk_rule_escalation_recipient_contact TO fk_rule_entry_recipient_contact;
ALTER TABLE rule_entry_recipient RENAME CONSTRAINT fk_rule_escalation_recipient_contactgroup TO fk_rule_entry_recipient_contactgroup;
ALTER TABLE rule_entry_recipient RENAME CONSTRAINT fk_rule_escalation_recipient_schedule TO fk_rule_entry_recipient_schedule;
ALTER TABLE rule_entry_recipient RENAME CONSTRAINT fk_rule_escalation_recipient_channel TO fk_rule_entry_recipient_channel;

-- Restore the not null clauses on the renamed table again.
ALTER TABLE rule_entry_recipient
    ALTER COLUMN id SET NOT NULL,
    ALTER COLUMN rule_entry_id SET NOT NULL,
    ALTER COLUMN changed_at SET NOT NULL,
    ALTER COLUMN deleted SET NOT NULL,
    ADD CONSTRAINT pk_rule_entry_recipient PRIMARY KEY (id),
    ADD CONSTRAINT fk_rule_entry_recipient_rule_entry FOREIGN KEY (rule_entry_id) REFERENCES rule_entry(id);

ALTER TABLE incident_rule_escalation_state
    DROP CONSTRAINT pk_incident_rule_escalation_state CASCADE, -- CASCADE to incident_history table.
    ALTER COLUMN rule_escalation_id DROP NOT NULL;

ALTER TABLE incident_rule_escalation_state RENAME COLUMN rule_escalation_id TO rule_entry_id;
ALTER TABLE incident_rule_escalation_state
    ADD CONSTRAINT fk_incident_rule_escalation_state_rule_entry FOREIGN KEY (rule_entry_id) REFERENCES rule_entry(id),
    ADD CONSTRAINT pk_incident_rule_escalation_state PRIMARY KEY (incident_id, rule_entry_id),
    ALTER COLUMN rule_entry_id SET NOT NULL;

ALTER TABLE incident_history RENAME COLUMN rule_escalation_id TO rule_entry_id;
ALTER TABLE incident_history
    ADD CONSTRAINT fk_incident_history_rule_entry FOREIGN KEY (rule_entry_id) REFERENCES rule_entry(id),
    ADD CONSTRAINT fk_incident_history_incident_rule_escalation_state FOREIGN KEY (incident_id, rule_entry_id) REFERENCES incident_rule_escalation_state(incident_id, rule_entry_id);

TRUNCATE skipped_notification_history;
ALTER TABLE skipped_notification_history DROP COLUMN rule_escalation_id;
ALTER TABLE skipped_notification_history ADD COLUMN rule_entry_id bigint NOT NULL;

INSERT INTO notifications_schema(version, timestamp) VALUES('v0.2.0-25', EXTRACT(EPOCH from NOW()) * 1000);

-- Improved Default Notifications

SELECT assert_correct_schema_version('v0.2.0-25');

ALTER TABLE incident ADD COLUMN summary text;
ALTER TABLE incident ADD COLUMN source_id bigint;

UPDATE incident SET
    summary = COALESCE(NULLIF(LEFT(BTRIM(SPLIT_PART(incident.message, E'\n', 1)), 1024), ''), 'no summary'),
    source_id = (SELECT MIN(os.source_id) FROM object_source os WHERE os.object_id = incident.object_id);

ALTER TABLE incident ALTER COLUMN summary SET NOT NULL;
ALTER TABLE incident ALTER COLUMN source_id SET NOT NULL;

ALTER TABLE incident ADD CONSTRAINT fk_incident_source FOREIGN KEY (source_id) REFERENCES source(id);
CREATE INDEX idx_incident_source_id ON incident(source_id);

INSERT INTO notifications_schema(version, timestamp) VALUES('v0.2.0-26', EXTRACT(EPOCH from NOW()) * 1000);

-- channel: let Icinga Notifications manage plugin state & delivery results

SELECT assert_correct_schema_version('v0.2.0-26');

TRUNCATE TABLE channel_state;
ALTER TABLE channel_state
    ALTER COLUMN state_key TYPE uuid USING state_key::uuid,
    ADD COLUMN incident_id bigint NOT NULL,
    ADD CONSTRAINT fk_channel_state_incident FOREIGN KEY (incident_id) REFERENCES incident(id),
    DROP CONSTRAINT pk_channel_state,
    ADD CONSTRAINT pk_channel_state PRIMARY KEY (state_key);
CREATE INDEX idx_channel_state_incident_id ON channel_state(incident_id);

-- The result of the notification delivery attempt (if any) as a JSON string.
ALTER TABLE notification_history ADD COLUMN delivery_result text;

INSERT INTO notifications_schema(version, timestamp) VALUES('v0.2.0-27', EXTRACT(EPOCH from NOW()) * 1000);

-- Reduce severities to ok, warning, error and critical

SELECT assert_correct_schema_version('v0.2.0-27');

-- The severities were reduced to ok, warning, error and critical. The obsolete severities debug, info and notice are
-- mapped to ok, alert and emerg to critical, while err and crit were renamed to error and critical respectively.
ALTER TYPE severity RENAME TO severity_old;
CREATE TYPE severity AS ENUM ('ok', 'warning', 'error', 'critical');

ALTER TABLE incident
    ALTER COLUMN severity TYPE severity USING (CASE
        WHEN severity IN ('debug', 'info', 'notice') THEN 'ok'
        WHEN severity = 'err' THEN 'error'
        WHEN severity IN ('crit', 'alert', 'emerg') THEN 'critical'
        ELSE severity::text
    END)::severity;

ALTER TABLE incident_history
    ALTER COLUMN new_severity TYPE severity USING (CASE
        WHEN new_severity IN ('debug', 'info', 'notice') THEN 'ok'
        WHEN new_severity = 'err' THEN 'error'
        WHEN new_severity IN ('crit', 'alert', 'emerg') THEN 'critical'
        ELSE new_severity::text
    END)::severity,
    ALTER COLUMN old_severity TYPE severity USING (CASE
        WHEN old_severity IN ('debug', 'info', 'notice') THEN 'ok'
        WHEN old_severity = 'err' THEN 'error'
        WHEN old_severity IN ('crit', 'alert', 'emerg') THEN 'critical'
        ELSE old_severity::text
    END)::severity;

DROP TYPE severity_old;

-- Escalation conditions may reference severities both in their JSON AST (e.g. "value":"crit") and in their query
-- string (e.g. "qs":"incident_severity>=crit"). Since the only other supported attributes are incident_age and
-- is_managed, these severity names cannot occur as any other value and can thus safely be replaced.
UPDATE rule_entry
    SET condition = regexp_replace(
        regexp_replace(condition, '"(debug|info|notice)"', '"ok"', 'g'),
        '(?<=[=<>])(debug|info|notice)(?=[&|)"]|$)', 'ok', 'g'
    )
    WHERE condition LIKE '%incident_severity%';

UPDATE rule_entry
    SET condition = regexp_replace(
        regexp_replace(condition, '"err"', '"error"', 'g'),
        '(?<=[=<>])err(?=[&|)"]|$)', 'error', 'g'
    )
    WHERE condition LIKE '%incident_severity%';

UPDATE rule_entry
    SET condition = regexp_replace(
        regexp_replace(condition, '"(crit|alert|emerg)"', '"critical"', 'g'),
        '(?<=[=<>])(crit|alert|emerg)(?=[&|)"]|$)', 'critical', 'g'
    )
    WHERE condition LIKE '%incident_severity%';

INSERT INTO notifications_schema(version, timestamp) VALUES('v0.2.0-28', EXTRACT(EPOCH from NOW()) * 1000);

-- Attach event summary to each notification history entry

SELECT assert_correct_schema_version('v0.2.0-28');

ALTER TABLE notification_history ADD COLUMN event_summary text;
UPDATE notification_history SET event_summary = COALESCE(NULLIF(LEFT(BTRIM(SPLIT_PART(event_message, E'\n', 1)), 1024), ''), 'no summary');
ALTER TABLE notification_history ALTER COLUMN event_summary SET NOT NULL;

INSERT INTO notifications_schema(version, timestamp) VALUES('v0.2.0-29', EXTRACT(EPOCH from NOW()) * 1000);

-- Bump schema version

INSERT INTO notifications_schema(version, timestamp) VALUES('v1.0.0', EXTRACT(EPOCH from NOW()) * 1000);
