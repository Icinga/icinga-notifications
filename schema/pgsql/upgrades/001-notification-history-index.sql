SELECT assert_correct_schema_version('v1.0.0');

CREATE INDEX idx_notification_history_incident_id_triggered_at ON notification_history(incident_id, triggered_at DESC);

INSERT INTO notifications_schema(version, timestamp) VALUES('v1.0.0-1', EXTRACT(EPOCH from NOW()) * 1000);
