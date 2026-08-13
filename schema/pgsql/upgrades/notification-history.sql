CREATE TYPE notification_history_state_type AS ENUM ( 'sent', 'failed' );

ALTER TABLE incident_history ADD COLUMN event_id uuid; -- used for external references, lower case
ALTER TABLE event_queue ALTER COLUMN id SET DATA TYPE uuid USING (id::uuid);

CREATE TABLE notification_history (
    id bigserial,
    incident_history_id bigint NOT NULL,
    event_id uuid NOT NULL,
    contact_id bigint NOT NULL,
    contactgroup_id bigint,
    schedule_id bigint,
    channel_id bigint NOT NULL,
    incident_id bigint NOT NULL,
    message text NOT NULL,
    state notification_history_state_type NOT NULL,
    triggered_at bigint NOT NULL,
    source_id bigint NOT NULL,

    CONSTRAINT pk_notification_history PRIMARY KEY (id),
    CONSTRAINT fk_notification_history_contact FOREIGN KEY (contact_id) REFERENCES contact(id),
    CONSTRAINT fk_notification_history_contactgroup FOREIGN KEY (contactgroup_id) REFERENCES contactgroup(id),
    CONSTRAINT fk_notification_history_schedule FOREIGN KEY (schedule_id) REFERENCES schedule(id),
    CONSTRAINT fk_notification_history_channel FOREIGN KEY (channel_id) REFERENCES channel(id),
    CONSTRAINT fk_notification_history_incident FOREIGN KEY (incident_id) REFERENCES incident(id),
    CONSTRAINT fk_notification_history_incident_history FOREIGN KEY (incident_history_id) REFERENCES incident_history(id),
    CONSTRAINT fk_notification_history_source FOREIGN KEY (source_id) REFERENCES source(id)
);

CREATE TABLE skipped_notification_history (
    id bigserial,
    notification_history_id bigint NOT NULL,
    rule_id bigint NOT NULL,
    rule_escalation_id bigint NOT NULL,
    contactgroup_id bigint,
    schedule_id bigint,

    CONSTRAINT pk_skipped_notification_history PRIMARY KEY (id),
    CONSTRAINT fk_skipped_notification_history_notification FOREIGN KEY (notification_history_id) REFERENCES incident_history(id),
    CONSTRAINT fk_skipped_notification_history_rule FOREIGN KEY (rule_id) REFERENCES rule(id),
    CONSTRAINT fk_skipped_notification_history_rule_escalation FOREIGN KEY (rule_escalation_id) REFERENCES rule_escalation(id),
    CONSTRAINT fk_skipped_notification_history_contactgroup FOREIGN KEY (contactgroup_id) REFERENCES contactgroup(id),
    CONSTRAINT fk_skipped_notification_history_schedule FOREIGN KEY (schedule_id) REFERENCES schedule(id)
);

CREATE INDEX idx_skipped_notification_history_incident_id ON skipped_notification_history(notification_history_id);
