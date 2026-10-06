CALL assert_correct_schema_version('v0.2.0-10');

ALTER TABLE incident_contact
  ADD COLUMN id bigserial,
  ADD CONSTRAINT pk_incident_contact PRIMARY KEY (id);

INSERT INTO notifications_schema(version, timestamp) VALUES('v0.2.0-11', EXTRACT(EPOCH from NOW()) * 1000);
