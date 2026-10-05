-- Upgrade completes ha-init.sql.

ALTER TABLE event_queue
	DROP INDEX idx_event_queue_time,
	DROP INDEX idx_event_queue_time_state,
	RENAME COLUMN time TO last_update,
	ADD COLUMN event_time bigint NULL;

UPDATE event_queue SET event_time = last_update;

ALTER TABLE event_queue MODIFY COLUMN event_time bigint NOT NULL;

CREATE INDEX idx_event_queue_last_update ON event_queue (last_update);
CREATE INDEX idx_event_queue_last_update_state ON event_queue (last_update, state);
