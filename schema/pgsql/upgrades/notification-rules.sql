CREATE TYPE rule_type AS ENUM ('notification', 'escalation');
ALTER TABLE rule ADD COLUMN type rule_type;
UPDATE rule SET type = 'escalation' WHERE type IS NULL;
ALTER TABLE rule ALTER COLUMN type SET NOT NULL;

-- Rename rule_escalation to rule_entry.
ALTER TABLE rule_escalation RENAME TO rule_entry;
ALTER SEQUENCE rule_escalation_id_seq RENAME TO rule_entry_id_seq;
ALTER TABLE rule_entry RENAME CONSTRAINT pk_rule_escalation TO pk_rule_entry;
ALTER TABLE rule_entry RENAME CONSTRAINT uk_rule_escalation_rule_id_position TO uk_rule_entry_rule_id_position;
ALTER TABLE rule_entry RENAME CONSTRAINT ck_rule_escalation_not_both_condition_and_fallback_for TO ck_rule_entry_not_both_condition_and_fallback_for;
ALTER TABLE rule_entry RENAME CONSTRAINT ck_rule_escalation_non_deleted_needs_position TO ck_rule_entry_non_deleted_needs_position;
ALTER TABLE rule_entry RENAME CONSTRAINT fk_rule_escalation_rule TO fk_rule_entry_rule;
ALTER TABLE rule_entry RENAME CONSTRAINT fk_rule_escalation_rule_escalation TO fk_rule_entry_rule_entry;
ALTER INDEX idx_rule_escalation_changed_at RENAME TO idx_rule_entry_changed_at;

-- Rename rule_escalation_recipient to rule_entry_recipient.
ALTER TABLE rule_escalation_recipient RENAME TO rule_entry_recipient;
ALTER SEQUENCE rule_escalation_recipient_id_seq RENAME TO rule_entry_recipient_id_seq;
ALTER TABLE rule_entry_recipient RENAME COLUMN rule_escalation_id TO rule_entry_id;
ALTER TABLE rule_entry_recipient RENAME CONSTRAINT pk_rule_escalation_recipient TO pk_rule_entry_recipient;
ALTER TABLE rule_entry_recipient RENAME CONSTRAINT ck_rule_escalation_recipient_has_exactly_one_recipient TO ck_rule_entry_recipient_has_exactly_one_recipient;
ALTER TABLE rule_entry_recipient RENAME CONSTRAINT fk_rule_escalation_recipient_rule_escalation TO fk_rule_entry_recipient_rule_entry;
ALTER TABLE rule_entry_recipient RENAME CONSTRAINT fk_rule_escalation_recipient_contact TO fk_rule_entry_recipient_contact;
ALTER TABLE rule_entry_recipient RENAME CONSTRAINT fk_rule_escalation_recipient_contactgroup TO fk_rule_entry_recipient_contactgroup;
ALTER TABLE rule_entry_recipient RENAME CONSTRAINT fk_rule_escalation_recipient_schedule TO fk_rule_entry_recipient_schedule;
ALTER TABLE rule_entry_recipient RENAME CONSTRAINT fk_rule_escalation_recipient_channel TO fk_rule_entry_recipient_channel;
ALTER INDEX idx_rule_escalation_recipient_changed_at RENAME TO idx_rule_entry_recipient_changed_at;

-- Rename incident_rule_escalation_state to incident_rule_entry_state.
ALTER TABLE incident_rule_escalation_state RENAME TO incident_rule_entry_state;
ALTER TABLE incident_rule_entry_state RENAME COLUMN rule_escalation_id TO rule_entry_id;
ALTER TABLE incident_rule_entry_state RENAME CONSTRAINT pk_incident_rule_escalation_state TO pk_incident_rule_entry_state;
ALTER TABLE incident_rule_entry_state RENAME CONSTRAINT fk_incident_rule_escalation_state_incident TO fk_incident_rule_entry_state_incident;
ALTER TABLE incident_rule_entry_state RENAME CONSTRAINT fk_incident_rule_escalation_state_rule_escalation TO fk_incident_rule_entry_state_rule_entry;
ALTER INDEX idx_incident_rule_escalation_state_incident_id RENAME TO idx_incident_rule_entry_state_incident_id;

-- Rename incident_history.rule_escalation_id to rule_entry_id.
ALTER TABLE incident_history RENAME COLUMN rule_escalation_id TO rule_entry_id;
ALTER TABLE incident_history RENAME CONSTRAINT fk_incident_history_incident_rule_escalation_state TO fk_incident_history_incident_rule_entry_state;
ALTER TABLE incident_history RENAME CONSTRAINT fk_incident_history_rule_escalation TO fk_incident_history_rule_entry;

-- Rename skipped_notification_history.rule_escalation_id to rule_entry_id.
ALTER TABLE skipped_notification_history RENAME COLUMN rule_escalation_id TO rule_entry_id;

-- PostgreSQL >= 17 stores NOT NULL constraints as named catalog objects (pg_constraint, contype = 'n'),
-- auto-named after the table/column at creation time. Renaming a table/column above does not rename
-- these, so they would keep referring to the old rule_escalation(_recipient)/incident_rule_escalation_state
-- names. Bring them in line with what a fresh install of schema.sql would produce. This is a no-op on
-- PostgreSQL < 17, where NOT NULL constraints aren't catalogued under these names.
DO $$
DECLARE
    renames CONSTANT text[][] := ARRAY[
        ['rule_entry', 'rule_escalation_id_not_null', 'rule_entry_id_not_null'],
        ['rule_entry', 'rule_escalation_rule_id_not_null', 'rule_entry_rule_id_not_null'],
        ['rule_entry', 'rule_escalation_changed_at_not_null', 'rule_entry_changed_at_not_null'],
        ['rule_entry', 'rule_escalation_deleted_not_null', 'rule_entry_deleted_not_null'],
        ['rule_entry_recipient', 'rule_escalation_recipient_id_not_null', 'rule_entry_recipient_id_not_null'],
        ['rule_entry_recipient', 'rule_escalation_recipient_rule_escalation_id_not_null', 'rule_entry_recipient_rule_entry_id_not_null'],
        ['rule_entry_recipient', 'rule_escalation_recipient_changed_at_not_null', 'rule_entry_recipient_changed_at_not_null'],
        ['rule_entry_recipient', 'rule_escalation_recipient_deleted_not_null', 'rule_entry_recipient_deleted_not_null'],
        ['incident_rule_entry_state', 'incident_rule_escalation_state_incident_id_not_null', 'incident_rule_entry_state_incident_id_not_null'],
        ['incident_rule_entry_state', 'incident_rule_escalation_state_rule_escalation_id_not_null', 'incident_rule_entry_state_rule_entry_id_not_null'],
        ['incident_rule_entry_state', 'incident_rule_escalation_state_triggered_at_not_null', 'incident_rule_entry_state_triggered_at_not_null'],
        ['skipped_notification_history', 'skipped_notification_history_rule_escalation_id_not_null', 'skipped_notification_history_rule_entry_id_not_null']
    ];
    r text[];
BEGIN
    FOREACH r SLICE 1 IN ARRAY renames LOOP
        IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname = r[2] AND conrelid = r[1]::regclass) THEN
            EXECUTE format('ALTER TABLE %I RENAME CONSTRAINT %I TO %I', r[1], r[2], r[3]);
        END IF;
    END LOOP;
END;
$$;
