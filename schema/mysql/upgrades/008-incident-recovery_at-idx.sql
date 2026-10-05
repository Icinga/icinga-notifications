CALL assert_correct_schema_version('v0.2.0-7');

CREATE INDEX idx_incident_recovered_at ON incident(recovered_at);

INSERT INTO notifications_schema(version, timestamp) VALUES('v0.2.0-8', UNIX_TIMESTAMP() * 1000);
