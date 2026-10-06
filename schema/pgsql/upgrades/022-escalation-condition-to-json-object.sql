SELECT assert_correct_schema_version('v0.2.0-21');

-- This script is used to upgrade the escalation condition column in the rule_escalation table from a raw filter
-- string to the new JSON object format. The raw filter string is a simple string that represents a logical condition,
-- while the new JSON object format is a structured representation of the same condition. For filter chains, this
-- script assumes that the logical operator is always &, so if somehow a different logical op is used, the resulting
-- JSON object will be invalid.
CREATE OR REPLACE FUNCTION upgrade_escalation_condition_to_json_object()
    RETURNS void
    LANGUAGE plpgsql
    STRICT
    PARALLEL RESTRICTED
AS $$
DECLARE
    rec RECORD;
    parts text[]; -- Array to hold the parts of the condition (attribute, operator, value)
    cond text; -- Variable to hold each individual condition when splitting by &
    json_rules jsonb; -- JSON array to hold the converted rules
BEGIN
    <<records_loop>>
    FOR rec IN (SELECT id, condition FROM rule_escalation WHERE condition IS NOT NULL) LOOP
        json_rules := '[]'::jsonb;

        -- If the raw filter string doesn't contain the logical operator &, we only have a single condition,
        -- so we need to directly convert it to a JSON literal similar to this:
        -- {...,"ast":{"op":"!=","attributes":["is_managed"],"value":"n"}}
        IF position('&' IN rec.condition) = 0 THEN
            parts := regexp_matches(rec.condition, '([^!=><]+)(!=|=|>|<|>=|<=)(.*)');
            -- If the extracted parts array doesn't have exactly 3 elements, we have an invalid condition, so skip this record.
            IF array_length(parts, 1) <> 3 THEN
                CONTINUE records_loop; -- We have an invalid condition, so skip this record.
            END IF;
            UPDATE rule_escalation SET condition = jsonb_build_object(
                'version', 2,
                'qs', rec.condition,
                'ast', jsonb_build_object(
                    'op', parts[2],
                    'attributes', jsonb_build_array(parts[1]),
                    'value', parts[3]
                )
            )::text
            WHERE id = rec.id;
        ELSE
            -- Otherwise, we have a & filter chain, so we need to split the raw filter string into individual
            -- conditions and convert each one to a final JSON object similar to this:
            -- {...,"ast":{"op":"&","rules":[{"op":">=","attributes":["incident_age"],"value":"1h"},...]}}
            FOREACH cond IN ARRAY string_to_array(rec.condition, '&') LOOP
                parts := regexp_matches(cond, '([^!=><]+)(!=|=|>|<|>=|<=)(.*)');
                IF array_length(parts, 1) != 3 THEN
                    CONTINUE records_loop; -- We have an invalid condition, so skip this record.
                END IF;
                json_rules := json_rules || jsonb_build_object(
                    'op', parts[2],
                    'attributes', jsonb_build_array(parts[1]),
                    'value', parts[3]
                );
            END LOOP;

            -- Finally, construct the final JSON object with the logical operator & and the array of converted rules,
            -- and update the corresponding tuple in the table with the resulting JSON object.
            UPDATE rule_escalation SET condition = jsonb_build_object(
                'version', 2,
                'qs', rec.condition,
                'ast', jsonb_build_object('op', '&', 'rules', json_rules)
            )::text
            WHERE id = rec.id;
        END IF;
    END LOOP;
END;
$$;

-- Run the upgrade and then drop the routine immediately as it is no longer needed after the upgrade is complete.
SELECT upgrade_escalation_condition_to_json_object();
DROP FUNCTION upgrade_escalation_condition_to_json_object();

INSERT INTO notifications_schema(version, timestamp) VALUES('v0.2.0-22', EXTRACT(EPOCH from NOW()) * 1000);
