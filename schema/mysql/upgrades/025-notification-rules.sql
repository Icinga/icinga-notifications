CALL assert_correct_schema_version('v0.2.0-24');

ALTER TABLE rule ADD COLUMN type enum('notification', 'escalation') AFTER source_type; -- NOT NULL is enforced via CHECK not to default to 'notification'
UPDATE rule SET type = 'escalation' WHERE type IS NULL;
ALTER TABLE rule ADD CONSTRAINT ck_rule_type_notnull CHECK (type IS NOT NULL);

ALTER TABLE incident_contact ADD COLUMN event_types text AFTER role;

-- Rename rule_escalation to rule_entry.
RENAME TABLE rule_escalation TO rule_entry;
ALTER TABLE rule_entry
    RENAME INDEX uk_rule_escalation_rule_id_position TO uk_rule_entry_rule_id_position,
    RENAME INDEX idx_rule_escalation_changed_at TO idx_rule_entry_changed_at,
    DROP FOREIGN KEY fk_rule_escalation_rule,
    DROP FOREIGN KEY fk_rule_escalation_rule_escalation,
    DROP CONSTRAINT ck_rule_escalation_not_both_condition_and_fallback_for,
    DROP CONSTRAINT ck_rule_escalation_non_deleted_needs_position,
    ADD CONSTRAINT ck_rule_entry_not_both_condition_and_fallback_for CHECK (NOT (`condition` IS NOT NULL AND fallback_for IS NOT NULL)),
    ADD CONSTRAINT ck_rule_entry_non_deleted_needs_position CHECK (deleted = 'y' OR position IS NOT NULL),
    ADD CONSTRAINT fk_rule_entry_rule FOREIGN KEY (rule_id) REFERENCES rule(id),
    ADD CONSTRAINT fk_rule_entry_rule_entry FOREIGN KEY (fallback_for) REFERENCES rule_entry(id);

-- Rename rule_escalation_recipient to rule_entry_recipient.
RENAME TABLE rule_escalation_recipient TO rule_entry_recipient;
ALTER TABLE rule_entry_recipient
    RENAME COLUMN rule_escalation_id TO rule_entry_id,
    RENAME INDEX idx_rule_escalation_recipient_changed_at TO idx_rule_entry_recipient_changed_at,
    DROP FOREIGN KEY fk_rule_escalation_recipient_rule_escalation,
    DROP FOREIGN KEY fk_rule_escalation_recipient_contact,
    DROP FOREIGN KEY fk_rule_escalation_recipient_contactgroup,
    DROP FOREIGN KEY fk_rule_escalation_recipient_schedule,
    DROP FOREIGN KEY fk_rule_escalation_recipient_channel,
    DROP CONSTRAINT ck_rule_escalation_recipient_has_exactly_one_recipient,
    ADD CONSTRAINT ck_rule_entry_recipient_has_exactly_one_recipient CHECK (if(contact_id IS NULL, 0, 1) + if(contactgroup_id IS NULL, 0, 1) + if(schedule_id IS NULL, 0, 1) = 1),
    ADD CONSTRAINT fk_rule_entry_recipient_rule_entry FOREIGN KEY (rule_entry_id) REFERENCES rule_entry(id),
    ADD CONSTRAINT fk_rule_entry_recipient_contact FOREIGN KEY (contact_id) REFERENCES contact(id),
    ADD CONSTRAINT fk_rule_entry_recipient_contactgroup FOREIGN KEY (contactgroup_id) REFERENCES contactgroup(id),
    ADD CONSTRAINT fk_rule_entry_recipient_schedule FOREIGN KEY (schedule_id) REFERENCES schedule(id),
    ADD CONSTRAINT fk_rule_entry_recipient_channel FOREIGN KEY (channel_id) REFERENCES channel(id);

ALTER TABLE incident_history
    DROP FOREIGN KEY fk_incident_history_incident_rule_escalation_state;

ALTER TABLE incident_rule_escalation_state
    DROP FOREIGN KEY fk_incident_rule_escalation_state_rule_escalation,
    DROP PRIMARY KEY,
    RENAME COLUMN rule_escalation_id TO rule_entry_id,
    ADD CONSTRAINT fk_incident_rule_escalation_state_rule_entry FOREIGN KEY (rule_entry_id) REFERENCES rule_entry(id),
    ADD CONSTRAINT pk_incident_rule_escalation_state PRIMARY KEY (incident_id, rule_entry_id);

-- Rename incident_history.rule_escalation_id to rule_entry_id.
ALTER TABLE incident_history
    RENAME COLUMN rule_escalation_id TO rule_entry_id,
    DROP FOREIGN KEY fk_incident_history_rule_escalation,
    ADD CONSTRAINT fk_incident_history_rule_entry FOREIGN KEY (rule_entry_id) REFERENCES rule_entry(id),
    ADD CONSTRAINT fk_incident_history_incident_rule_escalation_state FOREIGN KEY (incident_id, rule_entry_id) REFERENCES incident_rule_escalation_state(incident_id, rule_entry_id);

-- Rename skipped_notification_history.rule_escalation_id to rule_entry_id.
ALTER TABLE skipped_notification_history RENAME COLUMN rule_escalation_id TO rule_entry_id;

INSERT INTO notifications_schema(version, timestamp) VALUES('v0.2.0-25', UNIX_TIMESTAMP() * 1000);
