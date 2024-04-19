ALTER TYPE incident_history_event_type RENAME TO __incident_history_event_type;
CREATE TYPE incident_history_event_type AS ENUM (
    -- Order to be honored for events with identical microsecond timestamps.
    'opened',
    'incident_severity_changed',
    'rule_matched',
    'escalation_triggered',
    'recipient_role_changed',
    'closed',
    'notified'
);
ALTER TABLE incident_history ALTER type TYPE incident_history_event_type USING type::TEXT::incident_history_event_type;
DROP TYPE __incident_history_event_type;

CREATE INDEX idx_incident_history_time_type ON incident_history(time, type);
COMMENT ON INDEX idx_incident_history_time_type IS 'Incident History ordered by time/type';
