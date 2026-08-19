CREATE TABLE object_source (
    object_id bytea NOT NULL,
    source_id bigint NOT NULL, -- The specific source that the object referenced by object_id is associated with.

    CONSTRAINT pk_object_source PRIMARY KEY (object_id, source_id),
    CONSTRAINT fk_object_source_object FOREIGN KEY (object_id) REFERENCES object(id),
    CONSTRAINT fk_object_source_source FOREIGN KEY (source_id) REFERENCES source(id)
);
-- The object ID is going to be re-generated, so we need to clear the table and reset the sequence in a cascade manner.
TRUNCATE TABLE object RESTART IDENTITY CASCADE;
ALTER TABLE object
  DROP CONSTRAINT fk_object_source,
  DROP COLUMN source_id;

ALTER TABLE rule ADD COLUMN source_type text;
UPDATE rule SET source_type = source.type FROM source WHERE rule.source_id = source.id;
ALTER TABLE rule
  DROP CONSTRAINT fk_rule_source,
  DROP COLUMN source_id,
  ALTER COLUMN source_type SET NOT NULL;
