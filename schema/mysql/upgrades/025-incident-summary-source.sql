CALL assert_correct_schema_version('v0.2.0-24');

ALTER TABLE incident ADD COLUMN summary longtext;

ALTER TABLE incident ADD COLUMN source_id bigint;
UPDATE incident i SET i.source_id = (
    SELECT MIN(os.source_id) FROM object_source os WHERE os.object_id = i.object_id);
ALTER TABLE incident MODIFY COLUMN source_id bigint NOT NULL;
ALTER TABLE incident ADD CONSTRAINT fk_incident_source FOREIGN KEY (source_id) REFERENCES source(id);

INSERT INTO notifications_schema(version, timestamp) VALUES('v0.2.0-25', UNIX_TIMESTAMP() * 1000);
