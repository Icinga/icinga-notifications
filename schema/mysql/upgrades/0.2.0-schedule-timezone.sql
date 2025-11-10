ALTER TABLE schedule ADD COLUMN timezone text AFTER name;
UPDATE schedule SET timezone = (
    SELECT entry.timezone
    FROM timeperiod_entry entry
    INNER JOIN timeperiod ON timeperiod.id = entry.timeperiod_id
    INNER JOIN rotation ON rotation.id = timeperiod.owned_by_rotation_id
    WHERE rotation.schedule_id = schedule.id
    ORDER BY entry.id
    LIMIT 1
);
UPDATE schedule SET timezone = 'UTC' WHERE timezone IS NULL;
ALTER TABLE schedule MODIFY COLUMN timezone text NOT NULL;
