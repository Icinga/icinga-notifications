CALL assert_correct_schema_version('v0.2.0-3');

DROP TABLE incident_event;
ALTER TABLE incident_history DROP FOREIGN KEY fk_incident_history_event;
ALTER TABLE incident_history DROP INDEX fk_incident_history_event;
ALTER TABLE incident_history DROP COLUMN event_id;
DROP TABLE event;

INSERT INTO notifications_schema(version, timestamp) VALUES('v0.2.0-4', UNIX_TIMESTAMP() * 1000);
