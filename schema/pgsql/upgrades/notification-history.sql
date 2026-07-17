CREATE TYPE notification_delivery_state_type AS ENUM ( 'suppressed', 'pending', 'sent', 'failed', 'superfluous' );
CREATE TYPE delivery_trigger_reason AS ENUM (
    -- Order to be honored for events with identical millisecond timestamps.
    'incident_severity_changed',
    'escalation_triggered',
    'opened',
    'closed',
    'muted',
    'unmuted',
);

CREATE TABLE delivery_history (
    id bigserial,
    incident_id bigint NOT NULL,
    rule_id bigint,
    rule_escalation_id bigint,
    contact_id bigint,
    contactgroup_id bigint,
    channel_id bigint,
    schedule_id bigint,
    message text,
    reason delivery_trigger_reason NOT NULL,
    sent_at bigint,
    notification_state notification_delivery_state_type,

    CONSTRAINT pk_delivery_history PRIMARY KEY (id),
    CONSTRAINT fk_delivery_history_incident FOREIGN KEY (incident_id) REFERENCES incident(id),
    CONSTRAINT fk_delivery_history_rule FOREIGN KEY (rule_id) REFERENCES rule(id),
    CONSTRAINT fk_delivery_history_rule_escalation FOREIGN KEY (rule_escalation_id) REFERENCES rule_escalation(id),
    CONSTRAINT fk_delivery_history_contact FOREIGN KEY (contact_id) REFERENCES contact(id),
    CONSTRAINT fk_delivery_history_contactgroup FOREIGN KEY (contactgroup_id) REFERENCES contactgroup(id),
    CONSTRAINT fk_delivery_history_channel FOREIGN KEY (channel_id) REFERENCES channel(id),
    CONSTRAINT fk_delivery_history_schedule FOREIGN KEY (schedule_id) REFERENCES schedule(id)
);

CREATE INDEX idx_delivery_history_time ON delivery_history(sent_at);
CREATE INDEX idx_delivery_history_incident_id ON delivery_history(incident_id);
