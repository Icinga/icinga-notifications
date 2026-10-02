CREATE TABLE object_source (
    object_id binary(32) NOT NULL,
    source_id bigint NOT NULL, -- The specific source that the object referenced by object_id is associated with.

    CONSTRAINT pk_object_source PRIMARY KEY (object_id, source_id),
    CONSTRAINT fk_object_source_object FOREIGN KEY (object_id) REFERENCES object(id),
    CONSTRAINT fk_object_source_source FOREIGN KEY (source_id) REFERENCES source(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

DELETE FROM incident_history;
DELETE FROM incident_rule_escalation_state;
DELETE FROM incident_rule;
DELETE FROM incident_contact;
DELETE FROM incident;
DELETE FROM object_id_tag;
DELETE FROM object;
ALTER TABLE object
  DROP CONSTRAINT fk_object_source,
  DROP COLUMN source_id;

ALTER TABLE rule ADD COLUMN source_type text AFTER object_filter;
UPDATE rule
  INNER JOIN source ON source.id = rule.source_id
  SET rule.source_type = source.type;

ALTER TABLE rule
  DROP CONSTRAINT fk_rule_source,
  DROP COLUMN source_id,
  MODIFY COLUMN source_type text NOT NULL;
