ALTER TABLE rotation ALTER COLUMN mode SET DEFAULT '24-7';
ALTER TABLE event ALTER COLUMN event_type SET DEFAULT 'acknowledgement-cleared';
ALTER TABLE incident ALTER COLUMN severity SET DEFAULT 'ok';
ALTER TABLE incident_contact ALTER COLUMN role SET DEFAULT 'recipient';
ALTER TABLE incident_history ALTER COLUMN type SET DEFAULT 'opened';

ALTER TABLE available_channel_type ALTER COLUMN type TYPE varchar(255);
ALTER TABLE channel ALTER COLUMN type TYPE varchar(255);
ALTER TABLE contact ADD CHECK (length(username) <= 254);
ALTER TABLE contact_address ALTER COLUMN type TYPE varchar(255);
ALTER TABLE object_id_tag ALTER COLUMN tag TYPE varchar(255);
ALTER TABLE object_extra_tag ALTER COLUMN tag TYPE varchar(255);
ALTER TABLE browser_session ADD CHECK (length(username) <= 254);
