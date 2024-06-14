ALTER TABLE available_channel_type ALTER COLUMN type TYPE varchar(255);
ALTER TABLE channel ALTER COLUMN type TYPE varchar(255);
ALTER TABLE contact ADD CONSTRAINT ck_contact_username_up_to_254_chars CHECK (length(username) <= 254);
ALTER TABLE contact_address ALTER COLUMN type TYPE varchar(255);
ALTER TABLE object_id_tag ALTER COLUMN tag TYPE varchar(255);
ALTER TABLE object_extra_tag ALTER COLUMN tag TYPE varchar(255);
ALTER TABLE browser_session ADD CONSTRAINT ck_browser_session_username_up_to_254_chars CHECK (length(username) <= 254);
