ALTER TABLE schedule_member DROP CONSTRAINT schedule_member_pkey;
ALTER TABLE schedule_member ALTER COLUMN contact_id DROP NOT NULL;
ALTER TABLE schedule_member ALTER COLUMN contactgroup_id DROP NOT NULL;
ALTER TABLE schedule_member ADD CONSTRAINT schedule_member_schedule_id_timeperiod_id_contact_id_key UNIQUE (schedule_id, timeperiod_id, contact_id);
ALTER TABLE schedule_member ADD CONSTRAINT schedule_member_schedule_id_timeperiod_id_contactgroup_id_key UNIQUE (schedule_id, timeperiod_id, contactgroup_id);
