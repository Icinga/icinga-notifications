CALL assert_correct_schema_version('v1.0.0-1');

CREATE INDEX idx_incident_recovered_at_severity_started_at ON incident(recovered_at, severity DESC, started_at DESC);
DROP INDEX idx_incident_recovered_at ON incident;

INSERT INTO notifications_schema(version, timestamp) VALUES('v1.0.0-2', UNIX_TIMESTAMP() * 1000);
