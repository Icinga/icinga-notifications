ALTER TABLE rule_escalation_recipient ALTER COLUMN channel_type DROP NOT NULL;
ALTER TABLE contact ADD COLUMN default_channel text;

UPDATE contact SET default_channel = 'rocketchat' WHERE default_channel IS NULL;
ALTER TABLE contact ALTER COLUMN default_channel SET NOT NULL;
