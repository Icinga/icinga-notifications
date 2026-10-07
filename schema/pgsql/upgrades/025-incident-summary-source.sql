SELECT assert_correct_schema_version('v0.2.0-24');

ALTER TABLE incident ADD COLUMN summary text;

ALTER TABLE incident ADD COLUMN source_id bigint;
UPDATE incident SET source_id = (
    SELECT MIN(os.source_id) FROM object_source os WHERE os.object_id = incident.object_id);
ALTER TABLE incident ALTER COLUMN source_id SET NOT NULL;
ALTER TABLE incident ADD CONSTRAINT fk_incident_source FOREIGN KEY (source_id) REFERENCES source(id);
CREATE INDEX idx_incident_source_id ON incident(source_id);

INSERT INTO notifications_schema(version, timestamp) VALUES('v0.2.0-25', EXTRACT(EPOCH from NOW()) * 1000);
