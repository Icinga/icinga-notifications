-- This script is used to upgrade the escalation condition column in the rule_escalation table from a raw filter
-- string to the new JSON object format. The raw filter string is a simple string that represents a logical condition,
-- while the new JSON object format is a structured representation of the same condition. For filter chains, this
-- script assumes that the logical operator is always &, so if somehow a different logical op is used, the resulting
-- JSON object will be invalid.
DROP PROCEDURE IF EXISTS upgrade_escalation_condition_to_json_object;
DELIMITER //
CREATE PROCEDURE upgrade_escalation_condition_to_json_object()
    LANGUAGE SQL
    MODIFIES SQL DATA
BEGIN
    DECLARE escalation_id int;
    DECLARE escalation_cond text;
    DECLARE tmp_escalation_cond text;
    DECLARE attribute text;
    DECLARE operator text;
    DECLARE value text;
    DECLARE json_rules json;

    DECLARE done int;
    DECLARE cur CURSOR FOR SELECT id, `condition` FROM rule_escalation WHERE `condition` IS NOT NULL;
    -- Set up a handler to exit the loop when there are no more records to process.
    -- See https://dev.mysql.com/doc/refman/8.4/en/fetch.html
    DECLARE CONTINUE HANDLER FOR NOT FOUND SET done = 1;

    SET escalation_id = 0;
    SET done = 0;

    OPEN cur;
    records_loop: LOOP
        FETCH cur INTO escalation_id, escalation_cond;
        IF done THEN
            LEAVE records_loop;
        END IF;

        -- Reset the state variables for each iteration.
        SET json_rules = JSON_ARRAY();
        SET attribute = NULL;
        SET operator = NULL;
        SET value = NULL;

        IF INSTR(escalation_cond, '&') = 0 THEN
            SET attribute = REGEXP_SUBSTR(escalation_cond, '[^!=><]+');
            SET operator = REGEXP_SUBSTR(escalation_cond, '[!=><]+');
            IF attribute IS NULL OR operator IS NULL THEN
                ITERATE records_loop; -- We have an invalid condition, so skip this record.
            END IF;
            SET value = SUBSTR(escalation_cond, LENGTH(attribute) + LENGTH(operator) + 1);
            UPDATE rule_escalation SET `condition` = JSON_OBJECT(
                'version', 2,
                'qs', escalation_cond,
                'ast', JSON_OBJECT('op', operator, 'attributes', JSON_ARRAY(attribute), 'value', value)
            ) WHERE id = escalation_id;
        ELSE
            -- Otherwise, we have a & filter chain, so we need to split the raw filter string into individual
            -- conditions and convert each one to a JSON literal like this:
            -- {"ast":{"op":"&","rules":[{"op":">=","attributes":["incident_age"],"value":"1h"},...]}}
            SET tmp_escalation_cond = escalation_cond;
            WHILE tmp_escalation_cond IS NOT NULL AND tmp_escalation_cond != '' DO
                SET attribute = REGEXP_SUBSTR(tmp_escalation_cond, '[^!=><]+');
                SET operator = REGEXP_SUBSTR(tmp_escalation_cond, '[!=><]+');
                -- The value is located after the operator and before the next & or the end of the string. This is
                -- not same as the single condition case, because we need to get the value after the operator but
                -- before the next & or end of the string. So, we use a Perl-style lookbehind assertion[^1][^2][^3]
                -- to find the value after the operator and before the next & or end of the string.
                --
                -- [^1]: https://perldoc.perl.org/perlre#Lookaround-Assertions
                -- [^2]: https://mariadb.com/docs/server/reference/sql-functions/string-functions/regular-expressions-functions/pcre
                -- [^3]: MySQL 8.0 uses the ICU library for its REGEXP, which supports all PCRE features, including
                -- lookbehind assertions. https://unicode-org.github.io/icu/userguide/strings/regexp.html#regular-expression-operators
                SET value = REGEXP_SUBSTR(tmp_escalation_cond, CONCAT('(?<=', operator, ')[^&]+'));
                IF attribute IS NULL OR operator IS NULL OR value IS NULL THEN
                    ITERATE records_loop; -- We have an invalid condition, so skip this record.
                END IF;

                -- We need to use JSON_EXTRACT as a workaround to avoid double encoding the resulting JSON object
                -- when appending it to the json_rules array. Without this, the resulting JSON object would be double
                -- encoded causing the final filter chain construction to be invalid.
                SET json_rules = JSON_ARRAY_APPEND(json_rules, '$', JSON_EXTRACT(JSON_OBJECT('op', operator, 'attributes', JSON_ARRAY(attribute), 'value', value), '$'));
                -- Remove the processed condition from the tmp_escalation_cond string including the leading & operator.
                SET tmp_escalation_cond = TRIM(LEADING '&' FROM SUBSTRING(tmp_escalation_cond, LENGTH(attribute) + LENGTH(operator) + LENGTH(value) + 1));
            END WHILE;

            -- Finally, construct the final JSON object with the logical operator & and the array of converted rules,
            -- and update the corresponding tuple in the table with the resulting JSON object.
            UPDATE rule_escalation SET `condition` = JSON_OBJECT(
                'version', 2,
                'qs', escalation_cond,
                -- For the same reason as above, we need to extract the entire JSON object ($ is the root selector).
                'ast', JSON_EXTRACT(JSON_OBJECT('op', '&', 'rules', JSON_EXTRACT(json_rules, '$')), '$')
            ) WHERE id = escalation_id;
        END IF;
    END LOOP;
    CLOSE cur;
END //
DELIMITER ;

-- Call the procedure to perform the upgrade and then drop it immediately as it's no longer needed after it's completed.
CALL upgrade_escalation_condition_to_json_object();
DROP PROCEDURE upgrade_escalation_condition_to_json_object;
