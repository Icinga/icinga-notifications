ALTER TABLE incident_contact
  ADD COLUMN id bigserial,
  ADD CONSTRAINT pk_incident_contact PRIMARY KEY (id);
