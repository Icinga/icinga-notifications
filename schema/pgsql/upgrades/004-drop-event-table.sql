DROP TABLE incident_event;
ALTER TABLE incident_history DROP CONSTRAINT fk_incident_history_event;
ALTER TABLE incident_history DROP COLUMN event_id;
DROP TABLE event;
DROP TYPE IF EXISTS event_type;
