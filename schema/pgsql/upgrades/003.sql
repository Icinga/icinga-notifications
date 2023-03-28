ALTER TABLE contact_address ALTER COLUMN contact_id SET NOT NULL;
ALTER TABLE timeperiod DROP CONSTRAINT timeperiod_owned_by_schedule_id_fkey;
ALTER TABLE timeperiod ADD CONSTRAINT timeperiod_owned_by_schedule_id_fkey FOREIGN KEY (owned_by_schedule_id) REFERENCES schedule(id);
