CREATE TYPE rule_type AS ENUM ('escalation', 'routing');

ALTER TABLE rule ADD COLUMN type rule_type;
UPDATE rule SET type = 'escalation';
ALTER TABLE rule ALTER COLUMN type SET NOT NULL;

ALTER TABLE rule_escalation RENAME TO rule_entry;
ALTER SEQUENCE rule_escalation_id_seq RENAME TO rule_entry_id_seq;
ALTER TABLE rule_entry RENAME CONSTRAINT pk_rule_escalation TO pk_rule_entry;
ALTER TABLE rule_entry RENAME CONSTRAINT uk_rule_escalation_rule_id_position TO uk_rule_entry_rule_id_position;
ALTER TABLE rule_entry RENAME CONSTRAINT ck_rule_escalation_not_both_condition_and_fallback_for TO ck_rule_entry_not_both_condition_and_fallback_for;
ALTER TABLE rule_entry RENAME CONSTRAINT ck_rule_escalation_non_deleted_needs_position TO ck_rule_entry_non_deleted_needs_position;
ALTER TABLE rule_entry RENAME CONSTRAINT fk_rule_escalation_rule TO fk_rule_entry_rule;
ALTER TABLE rule_entry RENAME CONSTRAINT fk_rule_escalation_rule_escalation TO fk_rule_entry_rule_entry;

ALTER INDEX idx_rule_escalation_changed_at RENAME TO idx_rule_entry_changed_at;

ALTER TABLE rule_escalation_recipient RENAME TO rule_entry_recipient;
ALTER TABLE rule_entry_recipient RENAME COLUMN rule_escalation_id TO rule_entry_id;
ALTER SEQUENCE rule_escalation_recipient_id_seq RENAME TO rule_entry_recipient_id_seq;
ALTER TABLE rule_entry_recipient RENAME CONSTRAINT pk_rule_escalation_recipient TO pk_rule_entry_recipient;
ALTER TABLE rule_entry_recipient RENAME CONSTRAINT ck_rule_escalation_recipient_has_exactly_one_recipient TO ck_rule_entry_recipient_has_exactly_one_recipient;
ALTER TABLE rule_entry_recipient RENAME CONSTRAINT fk_rule_escalation_recipient_rule_escalation TO fk_rule_entry_recipient_rule_entry;
ALTER TABLE rule_entry_recipient RENAME CONSTRAINT fk_rule_escalation_recipient_contact TO fk_rule_entry_recipient_contact;
ALTER TABLE rule_entry_recipient RENAME CONSTRAINT fk_rule_escalation_recipient_contactgroup TO fk_rule_entry_recipient_contactgroup;
ALTER TABLE rule_entry_recipient RENAME CONSTRAINT fk_rule_escalation_recipient_schedule TO fk_rule_entry_recipient_schedule;
ALTER TABLE rule_entry_recipient RENAME CONSTRAINT fk_rule_escalation_recipient_channel TO fk_rule_entry_recipient_channel;

ALTER INDEX idx_rule_escalation_recipient_changed_at RENAME TO idx_rule_entry_recipient_changed_at;

ALTER TABLE incident_rule_escalation_state RENAME TO incident_rule_entry_state;
ALTER TABLE incident_rule_entry_state RENAME COLUMN rule_escalation_id TO rule_entry_id;
ALTER TABLE incident_rule_entry_state RENAME CONSTRAINT pk_incident_rule_escalation_state TO pk_incident_rule_entry_state;
ALTER TABLE incident_rule_entry_state RENAME CONSTRAINT fk_incident_rule_escalation_state_incident TO fk_incident_rule_entry_state_incident;
ALTER TABLE incident_rule_entry_state RENAME CONSTRAINT fk_incident_rule_escalation_state_rule_escalation TO fk_incident_rule_entry_state_rule_entry;

CREATE TABLE notification_history (
  id bigserial,
  incident_id bigint,
  rule_entry_id bigint,
  contact_id bigint,
  contactgroup_id bigint,
  schedule_id bigint,
  channel_id bigint,
  time bigint NOT NULL,
  notification_state notification_state_type,
  sent_at bigint,
  message text,

  CONSTRAINT pk_notification_history PRIMARY KEY (id),
  CONSTRAINT fk_notification_history_incident FOREIGN KEY (incident_id) REFERENCES incident(id),
  CONSTRAINT fk_notification_history_rule_entry FOREIGN KEY (rule_entry_id) REFERENCES rule_entry(id),
  CONSTRAINT fk_notification_history_contact FOREIGN KEY (contact_id) REFERENCES contact(id),
  CONSTRAINT fk_notification_history_contactgroup FOREIGN KEY (contactgroup_id) REFERENCES contactgroup(id),
  CONSTRAINT fk_notification_history_schedule FOREIGN KEY (schedule_id) REFERENCES schedule(id),
  CONSTRAINT fk_notification_history_channel FOREIGN KEY (channel_id) REFERENCES channel(id)
);

ALTER TABLE incident_history RENAME COLUMN rule_escalation_id TO rule_entry_id;
ALTER TABLE incident_history
  ADD COLUMN notification_history_id bigint,
  DROP COLUMN channel_id,
  DROP COLUMN notification_state,
  DROP COLUMN sent_at,
  ADD CONSTRAINT fk_incident_history_notification_history FOREIGN KEY (notification_history_id) REFERENCES notification_history(id);

ALTER TABLE incident_history RENAME CONSTRAINT fk_incident_history_incident_rule_escalation_state TO fk_incident_history_incident_rule_entry_state;
ALTER TABLE incident_history RENAME CONSTRAINT fk_incident_history_rule_escalation TO fk_incident_history_rule_entry;

DROP TABLE incident_event;
