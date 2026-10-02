CREATE TYPE notification_history_state_type AS ENUM ( 'sent', 'failed' );

ALTER TABLE incident_history ADD COLUMN event_id uuid; -- used for external references
-- This will be used by the query in the [Incident.RetriggerEscalations] method in icinga-notifications.
CREATE INDEX idx_incident_history_event_id_incident_id ON incident_history(event_id, incident_id);

TRUNCATE TABLE job_queue CASCADE;

ALTER TABLE job_processing_lock DROP COLUMN job_queue_id;
ALTER TABLE job_queue
  DROP COLUMN id,
  ADD COLUMN id uuid NOT NULL,
  ADD CONSTRAINT pk_job_queue PRIMARY KEY (id);

ALTER TABLE job_processing_lock
  ADD COLUMN job_queue_id uuid NOT NULL,
  ADD CONSTRAINT fk_job_processing_lock_job_queue FOREIGN KEY (job_queue_id) REFERENCES job_queue(id),
  ADD CONSTRAINT uk_job_processing_lock_job_queue_id UNIQUE (job_queue_id);

CREATE TABLE notification_history (
    id bigserial,
    object_id bytea NOT NULL,
    event_id uuid NOT NULL,
    contact_id bigint NOT NULL,
    contactgroup_id bigint,
    schedule_id bigint,
    channel_id bigint NOT NULL,
    incident_id bigint,
    event_message text NOT NULL,
    state notification_history_state_type NOT NULL,
    triggered_at bigint NOT NULL,

    CONSTRAINT pk_notification_history PRIMARY KEY (id),
    CONSTRAINT fk_notification_history_object_id FOREIGN KEY (object_id) REFERENCES object(id)
);

CREATE INDEX idx_notification_history_triggered_at ON notification_history(triggered_at);
CREATE INDEX idx_notification_history_state_triggered_at ON notification_history(state, triggered_at);
-- This index is required for the "object" retention query, which becomes quite unusable without it.
CREATE INDEX idx_notification_history_object_id ON notification_history(object_id);

CREATE TABLE skipped_notification_history (
    id bigserial,
    notification_history_id bigint NOT NULL, -- The actual notification due to which the notification was skipped.
    rule_id bigint NOT NULL,
    rule_escalation_id bigint NOT NULL,
    contactgroup_id bigint,
    schedule_id bigint,

    CONSTRAINT pk_skipped_notification_history PRIMARY KEY (id),
    CONSTRAINT fk_skipped_notification_history_notification_history FOREIGN KEY (notification_history_id) REFERENCES notification_history(id)
);

-- This index is required for the "notification history" retention query to identify and delete related references.
CREATE INDEX idx_skipped_notification_history_notification_history_id ON skipped_notification_history(notification_history_id);
