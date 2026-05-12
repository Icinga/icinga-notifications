DROP TABLE incident_event;
ALTER TABLE incident_history DROP FOREIGN KEY fk_incident_history_event;
ALTER TABLE incident_history DROP INDEX fk_incident_history_event;
ALTER TABLE incident_history DROP COLUMN event_id;
DROP TABLE event;
