CALL assert_correct_schema_version('v0.2.0-3');

DROP TABLE incident_event;
ALTER TABLE incident_history DROP CONSTRAINT fk_incident_history_event;
ALTER TABLE incident_history DROP COLUMN event_id;
DROP TABLE event;
DROP TYPE IF EXISTS event_type;

INSERT INTO notifications_schema(version, timestamp) VALUES('v0.2.0-4', EXTRACT(EPOCH from NOW()) * 1000);
