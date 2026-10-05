-- Upgrade completes ha-init.sql.

DROP INDEX idx_event_queue_time;
DROP INDEX idx_event_queue_time_state;

ALTER TABLE event_queue
	RENAME COLUMN time TO last_update;

ALTER TABLE event_queue
	ADD COLUMN event_time bigint NULL;

UPDATE event_queue SET event_time = last_update;

ALTER TABLE event_queue
	ALTER COLUMN event_time SET NOT NULL;

CREATE INDEX idx_event_queue_last_update ON event_queue (last_update);
CREATE INDEX idx_event_queue_last_update_state ON event_queue (last_update, state);
