CREATE TYPE rule_type AS ENUM ('notification', 'escalation');
ALTER TABLE rule ADD COLUMN type rule_type;
UPDATE rule SET type = 'escalation' WHERE type IS NULL;
ALTER TABLE rule ALTER COLUMN type SET NOT NULL;

ALTER TABLE incident_contact ADD COLUMN event_types text;

-- Recreate rule_escalation as rule_entry, carrying over the existing rows and their ids.
CREATE TABLE rule_entry (
    id bigserial,
    rule_id bigint NOT NULL,
    position integer,
    condition text,
    name citext, -- if not set, recipients are used as a fallback for display purposes
    fallback_for bigint,

    changed_at bigint NOT NULL,
    deleted boolenum NOT NULL DEFAULT 'n',

    CONSTRAINT pk_rule_entry PRIMARY KEY (id),

    -- Each position in an escalation can only be used once.
    -- Column position must be NULLed for deletion via "deleted = 'y'"
    CONSTRAINT uk_rule_entry_rule_id_position UNIQUE (rule_id, position),

    CONSTRAINT ck_rule_entry_not_both_condition_and_fallback_for CHECK (NOT (condition IS NOT NULL AND fallback_for IS NOT NULL)),
    CONSTRAINT ck_rule_entry_non_deleted_needs_position CHECK (deleted = 'y' OR position IS NOT NULL),
    CONSTRAINT fk_rule_entry_rule FOREIGN KEY (rule_id) REFERENCES rule(id),
    CONSTRAINT fk_rule_entry_rule_entry FOREIGN KEY (fallback_for) REFERENCES rule_entry(id)
);

CREATE INDEX idx_rule_entry_changed_at ON rule_entry(changed_at);

INSERT INTO rule_entry (id, rule_id, position, condition, name, fallback_for, changed_at, deleted)
    SELECT id, rule_id, position, condition, name, fallback_for, changed_at, deleted FROM rule_escalation;
SELECT setval(pg_get_serial_sequence('rule_entry', 'id'), coalesce(max(id), 1), max(id) IS NOT NULL) FROM rule_entry;

-- Recreate rule_escalation_recipient as rule_entry_recipient, carrying over the existing rows and their ids.
CREATE TABLE rule_entry_recipient (
    id bigserial,
    rule_entry_id bigint NOT NULL,
    contact_id bigint,
    contactgroup_id bigint,
    schedule_id bigint,
    channel_id bigint,

    changed_at bigint NOT NULL,
    deleted boolenum NOT NULL DEFAULT 'n',

    CONSTRAINT pk_rule_entry_recipient PRIMARY KEY (id),
    CONSTRAINT ck_rule_entry_recipient_has_exactly_one_recipient CHECK (num_nonnulls(contact_id, contactgroup_id, schedule_id) = 1),
    CONSTRAINT fk_rule_entry_recipient_rule_entry FOREIGN KEY (rule_entry_id) REFERENCES rule_entry(id),
    CONSTRAINT fk_rule_entry_recipient_contact FOREIGN KEY (contact_id) REFERENCES contact(id),
    CONSTRAINT fk_rule_entry_recipient_contactgroup FOREIGN KEY (contactgroup_id) REFERENCES contactgroup(id),
    CONSTRAINT fk_rule_entry_recipient_schedule FOREIGN KEY (schedule_id) REFERENCES schedule(id),
    CONSTRAINT fk_rule_entry_recipient_channel FOREIGN KEY (channel_id) REFERENCES channel(id)
);

CREATE INDEX idx_rule_entry_recipient_changed_at ON rule_entry_recipient(changed_at);

INSERT INTO rule_entry_recipient (id, rule_entry_id, contact_id, contactgroup_id, schedule_id, channel_id, changed_at, deleted)
    SELECT id, rule_escalation_id, contact_id, contactgroup_id, schedule_id, channel_id, changed_at, deleted FROM rule_escalation_recipient;
SELECT setval(pg_get_serial_sequence('rule_entry_recipient', 'id'), coalesce(max(id), 1), max(id) IS NOT NULL) FROM rule_entry_recipient;

-- Recreate incident_rule_escalation_state as incident_rule_entry_state, carrying over the existing rows.
CREATE TABLE incident_rule_entry_state (
    incident_id bigint NOT NULL,
    rule_entry_id bigint NOT NULL,
    triggered_at bigint NOT NULL,

    CONSTRAINT pk_incident_rule_entry_state PRIMARY KEY (incident_id, rule_entry_id),
    CONSTRAINT fk_incident_rule_entry_state_incident FOREIGN KEY (incident_id) REFERENCES incident(id),
    CONSTRAINT fk_incident_rule_entry_state_rule_entry FOREIGN KEY (rule_entry_id) REFERENCES rule_entry(id)
);

-- PostgreSQL doesn't automatically create an index for foreign keys, so we need to do this manually.
CREATE INDEX idx_incident_rule_entry_state_incident_id ON incident_rule_entry_state(incident_id);

INSERT INTO incident_rule_entry_state (incident_id, rule_entry_id, triggered_at)
    SELECT incident_id, rule_escalation_id, triggered_at FROM incident_rule_escalation_state;

-- Drop the old tables now that their data lives in rule_entry, rule_entry_recipient and
-- incident_rule_entry_state. CASCADE takes care of dropping incident_history's foreign keys pointing at
-- them; incident_history itself and its rows are kept.
DROP TABLE rule_escalation_recipient;
DROP TABLE incident_rule_escalation_state CASCADE;
DROP TABLE rule_escalation CASCADE;

-- Rename incident_history.rule_escalation_id to rule_entry_id and re-point its foreign keys at the
-- recreated tables; the referenced ids were carried over as-is, so the existing rows stay intact.
ALTER TABLE incident_history RENAME COLUMN rule_escalation_id TO rule_entry_id;
ALTER TABLE incident_history
    ADD CONSTRAINT fk_incident_history_incident_rule_entry_state FOREIGN KEY (incident_id, rule_entry_id) REFERENCES incident_rule_entry_state(incident_id, rule_entry_id),
    ADD CONSTRAINT fk_incident_history_rule_entry FOREIGN KEY (rule_entry_id) REFERENCES rule_entry(id);

TRUNCATE skipped_notification_history;
ALTER TABLE skipped_notification_history DROP COLUMN rule_escalation_id;
ALTER TABLE skipped_notification_history ADD COLUMN rule_entry_id bigint NOT NULL;
