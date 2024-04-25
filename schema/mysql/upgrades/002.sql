ALTER TABLE rule ADD COLUMN type enum('escalation', 'routing');
UPDATE rule SET type = 'escalation';
ALTER TABLE rule MODIFY COLUMN type enum('escalation', 'routing') NOT NULL;

ALTER TABLE rule_escalation RENAME TO rule_entry;
ALTER TABLE rule_entry
  DROP CONSTRAINT uk_rule_escalation_rule_id_position,
  DROP CONSTRAINT ck_rule_escalation_not_both_condition_and_fallback_for,
  DROP CONSTRAINT ck_rule_escalation_non_deleted_needs_position,
  DROP CONSTRAINT fk_rule_escalation_rule,
  DROP CONSTRAINT fk_rule_escalation_rule_escalation,
  DROP INDEX idx_rule_escalation_changed_at;

ALTER TABLE rule_entry
  ADD CONSTRAINT uk_rule_entry_rule_id_position UNIQUE (rule_id, position),
  ADD CONSTRAINT ck_rule_entry_not_both_condition_and_fallback_for CHECK (NOT (`condition` IS NOT NULL AND fallback_for IS NOT NULL)),
  ADD CONSTRAINT ck_rule_entry_non_deleted_needs_position CHECK (deleted = 'y' OR position IS NOT NULL),
  ADD CONSTRAINT fk_rule_entry_rule FOREIGN KEY (rule_id) REFERENCES rule(id),
  ADD CONSTRAINT fk_rule_entry_rule_entry FOREIGN KEY (fallback_for) REFERENCES rule_entry(id),
  ADD INDEX idx_rule_entry_changed_at (changed_at);

ALTER TABLE rule_escalation_recipient RENAME TO rule_entry_recipient;
ALTER TABLE rule_entry_recipient RENAME COLUMN rule_escalation_id TO rule_entry_id;
ALTER TABLE rule_entry_recipient
  DROP CONSTRAINT ck_rule_escalation_recipient_has_exactly_one_recipient,
  DROP CONSTRAINT fk_rule_escalation_recipient_rule_escalation,
  DROP CONSTRAINT fk_rule_escalation_recipient_contact,
  DROP CONSTRAINT fk_rule_escalation_recipient_contactgroup,
  DROP CONSTRAINT fk_rule_escalation_recipient_schedule,
  DROP CONSTRAINT fk_rule_escalation_recipient_channel,
  DROP INDEX idx_rule_escalation_recipient_changed_at;

ALTER TABLE rule_entry_recipient
  ADD CONSTRAINT ck_rule_entry_recipient_has_exactly_one_recipient CHECK (if(contact_id IS NULL, 0, 1) + if(contactgroup_id IS NULL, 0, 1) + if(schedule_id IS NULL, 0, 1) = 1),
  ADD CONSTRAINT fk_rule_entry_recipient_rule_entry FOREIGN KEY (rule_entry_id) REFERENCES rule_entry(id),
  ADD CONSTRAINT fk_rule_entry_recipient_contact FOREIGN KEY (contact_id) REFERENCES contact(id),
  ADD CONSTRAINT fk_rule_entry_recipient_contactgroup FOREIGN KEY (contactgroup_id) REFERENCES contactgroup(id),
  ADD CONSTRAINT fk_rule_entry_recipient_schedule FOREIGN KEY (schedule_id) REFERENCES schedule(id),
  ADD CONSTRAINT fk_rule_entry_recipient_channel FOREIGN KEY (channel_id) REFERENCES channel(id),
  ADD INDEX idx_rule_entry_recipient_changed_at (changed_at);

ALTER TABLE incident_rule_escalation_state RENAME TO incident_rule_entry_state;
ALTER TABLE incident_rule_entry_state RENAME COLUMN rule_escalation_id TO rule_entry_id;
ALTER TABLE incident_rule_entry_state
  DROP CONSTRAINT fk_incident_rule_escalation_state_incident,
  DROP CONSTRAINT fk_incident_rule_escalation_state_rule_escalation;

ALTER TABLE incident_rule_entry_state
  Add CONSTRAINT fk_incident_rule_entry_state_incident FOREIGN KEY (incident_id) REFERENCES incident(id),
  Add CONSTRAINT fk_incident_rule_entry_state_rule_entry FOREIGN KEY (rule_entry_id) REFERENCES rule_entry(id);

CREATE TABLE notification_history (
  id bigint NOT NULL AUTO_INCREMENT PRIMARY KEY,
  incident_id bigint,
  rule_entry_id bigint,
  contact_id bigint,
  contactgroup_id bigint,
  schedule_id bigint,
  channel_id bigint,
  time bigint NOT NULL,
  notification_state enum('suppressed', 'pending', 'sent', 'failed'),
  sent_at bigint,
  message mediumtext,

  CONSTRAINT fk_notification_history_incident FOREIGN KEY (incident_id) REFERENCES incident(id),
  CONSTRAINT fk_notification_history_rule_entry FOREIGN KEY (rule_entry_id) REFERENCES rule_entry(id),
  CONSTRAINT fk_notification_history_contact FOREIGN KEY (contact_id) REFERENCES contact(id),
  CONSTRAINT fk_notification_history_contactgroup FOREIGN KEY (contactgroup_id) REFERENCES contactgroup(id),
  CONSTRAINT fk_notification_history_schedule FOREIGN KEY (schedule_id) REFERENCES schedule(id),
  CONSTRAINT fk_notification_history_channel FOREIGN KEY (channel_id) REFERENCES channel(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;;

ALTER TABLE incident_history RENAME COLUMN rule_escalation_id TO rule_entry_id;
ALTER TABLE incident_history
  ADD COLUMN notification_history_id bigint AFTER rule_id,
  DROP CONSTRAINT fk_incident_history_channel,
  DROP COLUMN channel_id,
  DROP COLUMN notification_state,
  DROP COLUMN sent_at,
  DROP CONSTRAINT fk_incident_history_incident_rule_escalation_state,
  DROP CONSTRAINT fk_incident_history_rule_escalation;

ALTER TABLE incident_history
  Add CONSTRAINT fk_incident_history_incident_rule_entry_state FOREIGN KEY (incident_id, rule_entry_id) REFERENCES incident_rule_entry_state(incident_id, rule_entry_id),
  Add CONSTRAINT fk_incident_history_rule_entry FOREIGN KEY (rule_entry_id) REFERENCES rule_entry(id),
  ADD CONSTRAINT fk_incident_history_notification_history FOREIGN KEY (notification_history_id) REFERENCES notification_history(id);
