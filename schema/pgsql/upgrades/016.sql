ALTER TABLE contact ADD COLUMN default_channel_id bigint REFERENCES channel(id);
UPDATE contact SET default_channel_id = (SELECT id FROM channel WHERE channel.type = contact.default_channel LIMIT 1);
ALTER TABLE contact
  DROP COLUMN default_channel,
  ALTER COLUMN default_channel_id SET NOT NULL;

ALTER TABLE rule_escalation_recipient ADD COLUMN channel_id bigint REFERENCES channel(id);
UPDATE rule_escalation_recipient SET channel_id = (SELECT id FROM channel WHERE channel.type = rule_escalation_recipient.channel_type LIMIT 1);
ALTER TABLE rule_escalation_recipient DROP COLUMN channel_type;

ALTER TABLE incident_history ADD COLUMN channel_id bigint REFERENCES channel(id);
UPDATE incident_history SET channel_id = (SELECT id FROM channel WHERE channel.type = incident_history.channel_type LIMIT 1);
ALTER TABLE incident_history DROP COLUMN channel_type;
