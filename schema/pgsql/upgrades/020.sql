CREATE TABLE object_id_tag (
    object_id bytea NOT NULL REFERENCES object(id),
    tag text NOT NULL,
    value text NOT NULL,

    CONSTRAINT pk_object_id_tag PRIMARY KEY (object_id, tag)
);

INSERT INTO object_id_tag (object_id, tag, value) SELECT id, 'host', host FROM object;
INSERT INTO object_id_tag (object_id, tag, value) SELECT id, 'service', service FROM object WHERE service IS NOT NULL;

ALTER TABLE object
  DROP COLUMN host,
  DROP COLUMN service;
