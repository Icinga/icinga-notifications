ALTER TABLE rotation ALTER COLUMN mode SET DEFAULT '24-7';
ALTER TABLE event ALTER COLUMN event_type SET DEFAULT 'acknowledgement-cleared';
ALTER TABLE incident ALTER COLUMN severity SET DEFAULT 'ok';
ALTER TABLE incident_contact ALTER COLUMN role SET DEFAULT 'recipient';
ALTER TABLE incident_history ALTER COLUMN type SET DEFAULT 'opened';
