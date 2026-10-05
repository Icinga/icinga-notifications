ALTER TABLE incident_contact
  ADD COLUMN id bigint NOT NULL AUTO_INCREMENT FIRST,
  ADD CONSTRAINT pk_incident_contact PRIMARY KEY (id);
