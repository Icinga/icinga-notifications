SELECT assert_correct_schema_version('v1.0.0-1');

CREATE INDEX idx_incident_severity_started_at ON incident(severity DESC, started_at DESC) WHERE recovered_at IS NULL;

INSERT INTO notifications_schema(version, timestamp) VALUES('v1.0.0-2', EXTRACT(EPOCH from NOW()) * 1000);
