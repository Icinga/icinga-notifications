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
