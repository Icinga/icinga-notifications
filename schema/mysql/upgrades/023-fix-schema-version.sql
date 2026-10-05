-- Redefine assert_correct_schema_version() as it determined the latest schema version by timestamp, which isn't
-- unique if upgrades are applied in quick succession, and its error message exceeded MySQL's 128 character limit.
DROP PROCEDURE IF EXISTS assert_correct_schema_version;
DELIMITER //
CREATE PROCEDURE assert_correct_schema_version(expected_version text)
    READS SQL DATA
    COMMENT 'Asserts that the schema version in the database matches the expected version and raises an error if not.'
BEGIN
    DECLARE actual_version text;
    DECLARE error_message text;
    SELECT version INTO actual_version FROM notifications_schema ORDER BY id DESC LIMIT 1;
    IF actual_version IS NULL THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'Schema version not found in notifications_schema table.';
    ELSEIF actual_version != expected_version THEN
        -- MySQL/MariaDB doesn't seem to allow to directly use CONCAT in the SIGNAL statement[^1],
        -- so we need to set it to a variable first.
        -- [^1]: https://bugs.mysql.com/bug.php?id=114001
        SET error_message = CONCAT('Schema version mismatch: expected ', expected_version, ', got ', actual_version, '. Apply all previous upgrade scripts in order first.');
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = error_message;
    END IF;
END //
DELIMITER ;

-- Until the upgrade scripts stamped their own schema version, 003-schema-version-table.sql stamped v1.0. Databases
-- upgraded by then are assumed to have applied all upgrades preceding this one, so their version is corrected first.
UPDATE notifications_schema SET version = 'v0.2.0-22' WHERE version = 'v1.0';

CALL assert_correct_schema_version('v0.2.0-22');

INSERT INTO notifications_schema(version, timestamp) VALUES('v0.2.0-23', UNIX_TIMESTAMP() * 1000);
