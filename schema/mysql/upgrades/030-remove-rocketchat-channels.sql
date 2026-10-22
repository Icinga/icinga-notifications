CALL assert_correct_schema_version('v0.2.0-29');

-- The Rocket.Chat channel was removed. Abort if it is still in use, as there is no sensible channel to migrate to.
-- Contacts and event rules must be switched to another channel manually before applying this upgrade.
DROP PROCEDURE IF EXISTS assert_rocketchat_unused;
DELIMITER //
CREATE PROCEDURE assert_rocketchat_unused()
    READS SQL DATA
BEGIN
    IF EXISTS (
        SELECT 1
        FROM contact
        INNER JOIN channel ON channel.id = contact.default_channel_id
        WHERE contact.deleted = 'n' AND channel.type = 'rocketchat'
    ) THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'Contacts still use a Rocket.Chat default channel. Change it before applying this upgrade.';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM rule_entry_recipient
        INNER JOIN channel ON channel.id = rule_entry_recipient.channel_id
        INNER JOIN rule_entry ON rule_entry.id = rule_entry_recipient.rule_entry_id
        INNER JOIN rule ON rule.id = rule_entry.rule_id
        WHERE
            rule_entry_recipient.deleted = 'n' AND
            rule_entry.deleted = 'n' AND
            rule.deleted = 'n' AND
            channel.type = 'rocketchat'
    ) THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'Event rules still notify via a Rocket.Chat channel. Change it before applying this upgrade.';
    END IF;
END //
DELIMITER ;

-- Call the procedure to perform the check and then drop it immediately as it's no longer needed.
CALL assert_rocketchat_unused();
DROP PROCEDURE assert_rocketchat_unused;

UPDATE contact_address SET
  deleted = 'y',
  changed_at = UNIX_TIMESTAMP() * 1000
WHERE
  deleted = 'n' AND
  type = 'rocketchat';

DELETE FROM channel_state WHERE channel_id IN (SELECT id FROM channel WHERE type = 'rocketchat');

UPDATE channel SET
  deleted = 'y',
  external_uuid = NULL,
  config = '{}',
  validation_result = NULL,
  changed_at = UNIX_TIMESTAMP() * 1000
WHERE type = 'rocketchat';

INSERT INTO notifications_schema(version, timestamp) VALUES('v0.2.0-30', UNIX_TIMESTAMP() * 1000);
