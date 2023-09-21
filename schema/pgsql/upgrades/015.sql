ALTER TABLE object
  ADD COLUMN source_id bigint NOT NULL REFERENCES source(id) DEFAULT 1,
  ADD FOREIGN KEY (source_id) REFERENCES source(id);

ALTER TABLE event
  DROP CONSTRAINT event_source_id_fkey,
  DROP CONSTRAINT event_object_id_source_id_fkey,
  DROP COLUMN source_id;
