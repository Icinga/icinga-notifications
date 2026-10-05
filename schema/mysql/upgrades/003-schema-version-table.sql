DROP PROCEDURE IF EXISTS assert_correct_schema_version;
DELIMITER //
-- This procedure can be used in upgrade scripts to assert that the schema version in the database matches the
-- expected version before applying the upgrade. This is important to prevent users from accidentally skipping
-- intermediate upgrade scripts, which could lead to an inconsistent database state. For instance, since every
-- upgrade script knows its predecessor's version, we can just do "CALL assert_correct_schema_version('v1.0')"
-- at the beginning of the 1.x upgrade scripts to ensure that the 1.0 script has been applied before.
CREATE PROCEDURE assert_correct_schema_version(expected_version text)
    READS SQL DATA
    COMMENT 'Asserts that the schema version in the database matches the expected version and raises an error if not.'
BEGIN
    DECLARE actual_version text;
    DECLARE error_message text;
    SELECT version INTO actual_version FROM notifications_schema ORDER BY timestamp DESC LIMIT 1;
    IF actual_version IS NULL THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'Schema version not found in notifications_schema table.';
    ELSEIF actual_version != expected_version THEN
        SET error_message = CONCAT('Schema version mismatch: expected ', expected_version, ', got ', actual_version, '. Please apply all previous upgrade scripts in order before applying this one.');
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = error_message;
    END IF;
END //
DELIMITER ;

CREATE TABLE notifications_schema (
    id int NOT NULL AUTO_INCREMENT,
    version varchar(64) NOT NULL,
    timestamp bigint NOT NULL,

    CONSTRAINT pk_notifications_schema PRIMARY KEY (id),
    CONSTRAINT uk_notifications_schema_version UNIQUE (version)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

INSERT INTO notifications_schema(version, timestamp) VALUES('v1.0', UNIX_TIMESTAMP() * 1000);
