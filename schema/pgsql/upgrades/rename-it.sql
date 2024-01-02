CREATE TABLE rule_non_state_escalation
(
    id           bigserial,
    rule_id      bigint  NOT NULL REFERENCES rule (id),
    position     integer NOT NULL,
    condition    text,
    name         text, -- if not set, recipients are used as a fallback for display purposes
    fallback_for bigint REFERENCES rule_escalation (id),

    CONSTRAINT pk_non_state_escalation PRIMARY KEY (id),

    UNIQUE (rule_id, position),
    CHECK (NOT (condition IS NOT NULL AND fallback_for IS NOT NULL))
);

ALTER TABLE rule_escalation_recipient
    ALTER COLUMN rule_escalation_id DROP NOT NULL;
ALTER TABLE rule_escalation_recipient
    ADD COLUMN rule_non_state_escalation_id bigint REFERENCES rule_non_state_escalation (id);
ALTER TABLE rule_escalation_recipient
    DROP CONSTRAINT rule_escalation_recipient_check;
ALTER TABLE rule_escalation_recipient
    ADD CONSTRAINT rule_escalation_recipient_check
        CHECK (
                (num_nonnulls(contact_id, contactgroup_id, schedule_id) = 1)
                AND
                (num_nonnulls(rule_escalation_id, rule_non_state_escalation_id) = 1)
            );

ALTER TYPE incident_history_event_type RENAME TO history_event_type;

ALTER TABLE incident_history
    RENAME TO history;

ALTER TABLE history
    ALTER COLUMN type TYPE history_event_type;

ALTER TABLE history
    RENAME COLUMN caused_by_incident_history_id TO caused_by_history_id;

ALTER TABLE history
    ALTER COLUMN incident_id DROP NOT NULL;

ALTER TABLE history
    ADD COLUMN object_id bytea REFERENCES object(id);

UPDATE history h SET object_id = (SELECT object_id from incident i where i.id = h.incident_id);

ALTER TABLE history
    ALTER COLUMN object_id SET NOT NULL;
