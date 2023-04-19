CREATE TYPE incident_history_event_type AS ENUM ( 'source_severity_changed', 'incident_severity_changed', 'recipient_role_changed', 'escalation_triggered', 'rule_matched', 'opened', 'closed', 'notified' );
CREATE TYPE frequency_type AS ENUM ( 'MINUTELY', 'HOURLY', 'DAILY', 'WEEKLY', 'MONTHLY', 'QUARTERLY', 'YEARLY' );

ALTER TABLE contact ADD COLUMN color varchar(7);
ALTER TABLE contactgroup ADD COLUMN color varchar(7);
ALTER TABLE timeperiod_entry
    ADD COLUMN until_time bigint,
    ADD COLUMN frequency frequency_type;

ALTER TABLE incident_history
    ADD COLUMN contact_id bigint REFERENCES contact(id),
    ADD COLUMN event_id bigint REFERENCES event(id),
    ADD COLUMN contactgroup_id bigint REFERENCES contactgroup(id),
    ADD COLUMN schedule_id bigint REFERENCES schedule(id),
    ADD COLUMN rule_id bigint REFERENCES rule(id),
    ADD COLUMN caused_by_incident_history_id bigint REFERENCES incident_history(id),
    ADD COLUMN channel_type text,
    ADD COLUMN type incident_history_event_type NOT NULL,
    ADD COLUMN new_severity severity,
    ADD COLUMN old_severity severity,
    ADD COLUMN new_recipient_role incident_contact_role,
    ADD COLUMN old_recipient_role incident_contact_role;
