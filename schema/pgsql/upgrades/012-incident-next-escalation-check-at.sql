CALL assert_correct_schema_version('v0.2.0-11');

ALTER TABLE incident ADD COLUMN next_escalation_check_at bigint;
CREATE INDEX idx_incident_object_id_recovered_at ON incident(object_id, recovered_at);
CREATE INDEX idx_incident_next_escalation_check_at ON incident(next_escalation_check_at);
CREATE INDEX idx_incident_recovered_at_next_escalation_check_at ON incident(recovered_at, next_escalation_check_at);

INSERT INTO notifications_schema(version, timestamp) VALUES('v0.2.0-12', EXTRACT(EPOCH from NOW()) * 1000);
