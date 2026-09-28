SELECT assert_correct_schema_version('v0.2.0-23');

ALTER TABLE notification_history ADD COLUMN incident_closed boolenum NOT NULL DEFAULT 'n';

INSERT INTO notifications_schema(version, timestamp) VALUES('v0.2.0-24', EXTRACT(EPOCH from NOW()) * 1000);
