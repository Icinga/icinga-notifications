ALTER TABLE incident_history ADD COLUMN event_id binary(16) AFTER incident_id; -- used for external references
-- This will be used by the query in the [Incident.RetriggerEscalations] method in icinga-notifications.
CREATE INDEX idx_incident_history_event_id_incident_id ON incident_history(event_id, incident_id);

ALTER TABLE job_processing_lock DROP FOREIGN KEY fk_job_processing_lock_job_queue;

TRUNCATE TABLE job_processing_lock;
TRUNCATE TABLE job_queue;

ALTER TABLE job_queue MODIFY COLUMN id binary(16) NOT NULL;
ALTER TABLE job_processing_lock MODIFY COLUMN job_queue_id binary(16) NOT NULL;

ALTER TABLE job_processing_lock ADD CONSTRAINT fk_job_processing_lock_job_queue FOREIGN KEY (job_queue_id) REFERENCES job_queue(id);

CREATE TABLE notification_history (
    id bigint NOT NULL AUTO_INCREMENT,
    object_id binary(32) NOT NULL,
    event_id binary(16) NOT NULL,
    contact_id bigint NOT NULL,
    contactgroup_id bigint,
    schedule_id bigint,
    channel_id bigint NOT NULL,
    incident_id bigint,
    event_message text NOT NULL,
    state enum('sent', 'failed'),
    triggered_at bigint NOT NULL,

    CONSTRAINT pk_notification_history PRIMARY KEY (id),
    CONSTRAINT ck_notification_history_state_notnull CHECK (state IS NOT NULL),
    CONSTRAINT fk_notification_history_object_id FOREIGN KEY (object_id) REFERENCES object(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE INDEX idx_notification_history_triggered_at ON notification_history(triggered_at);
CREATE INDEX idx_notification_history_state_triggered_at ON notification_history(state, triggered_at);

CREATE TABLE skipped_notification_history (
    id bigint NOT NULL AUTO_INCREMENT,
    notification_history_id bigint NOT NULL, -- The actual notification due to which the notification was skipped.
    rule_id bigint NOT NULL,
    rule_escalation_id bigint NOT NULL,
    contactgroup_id bigint,
    schedule_id bigint,

    CONSTRAINT pk_skipped_notification_history PRIMARY KEY (id),
    CONSTRAINT fk_skipped_notification_history_notification_history FOREIGN KEY (notification_history_id) REFERENCES notification_history(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
