CREATE TABLE notification_history (
    id bigint NOT NULL AUTO_INCREMENT,
    incident_id bigint NOT NULL,
    rule_id bigint NOT NULL,
    rule_escalation_id bigint NOT NULL,
    contact_id bigint NOT NULL,
    channel_id bigint NOT NULL,
    contactgroup_id bigint,
    schedule_id bigint,
    message mediumtext,
    -- NOT NULL is enforced via CHECK not to default to 'incident_severity_changed'
    reason enum('incident_severity_changed', 'escalation_triggered', 'opened', 'closed', 'muted', 'unmuted'),
    state enum('suppressed', 'pending', 'sent', 'failed', 'superfluous'),
    triggered_at bigint NOT NULL,

    CONSTRAINT pk_notification_history PRIMARY KEY (id),
    CONSTRAINT ck_notification_history_type_notnull CHECK (reason IS NOT NULL),
    CONSTRAINT ck_notification_history_state_notnull CHECK (state IS NOT NULL),
    CONSTRAINT fk_notification_history_incident FOREIGN KEY (incident_id) REFERENCES incident(id),
    CONSTRAINT fk_notification_history_rule FOREIGN KEY (rule_id) REFERENCES rule(id),
    CONSTRAINT fk_notification_history_rule_escalation FOREIGN KEY (rule_escalation_id) REFERENCES rule_escalation(id),
    CONSTRAINT fk_notification_history_contact FOREIGN KEY (contact_id) REFERENCES contact(id),
    CONSTRAINT fk_notification_history_channel FOREIGN KEY (channel_id) REFERENCES channel(id),
    CONSTRAINT fk_notification_history_contactgroup FOREIGN KEY (contactgroup_id) REFERENCES contactgroup(id),
    CONSTRAINT fk_notification_history_schedule FOREIGN KEY (schedule_id) REFERENCES schedule(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE INDEX idx_notification_history_time ON notification_history(triggered_at);
CREATE INDEX idx_notification_history_incident_id ON notification_history(incident_id);
