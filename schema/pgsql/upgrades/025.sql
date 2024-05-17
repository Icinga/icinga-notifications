DO $$ BEGIN
    CREATE TYPE rotation_type AS ENUM ( '24-7', 'partial', 'multi' );
EXCEPTION
    WHEN duplicate_object THEN null;
END $$;

CREATE TABLE IF NOT EXISTS rotation (
    id bigserial,
    schedule_id bigint NOT NULL REFERENCES schedule(id),
    -- the lower the more important, starting at 0, avoids the need to re-index upon addition
    priority integer NOT NULL,
    name text NOT NULL,
    mode rotation_type NOT NULL,
    -- JSON with rotation-specific attributes
    -- Needed exclusively by Web to simplify editing and visualisation
    options text NOT NULL,

    -- A date in the format 'YYYY-MM-DD' when the first handoff should happen.
    -- It is a string as handoffs are restricted to happen only once per day
    first_handoff date NOT NULL,

    -- Set in case the first_handoff was in the past during creation of the rotation.
    -- It is essentially the creation time of the rotation.
    -- Used by Web to avoid showing shifts that never happened
    actual_handoff bigint,

    -- each schedule can only have one rotation with a given priority starting at given date
    UNIQUE (schedule_id, priority, first_handoff),

    CONSTRAINT pk_rotation PRIMARY KEY (id)
);

ALTER TABLE rule DROP COLUMN timeperiod_id;

DROP TABLE IF EXISTS schedule_member;
DROP TABLE IF EXISTS rotation_member;

DROP TABLE IF EXISTS timeperiod_entry;

DROP TABLE timeperiod;
CREATE TABLE timeperiod (
    id bigserial,
    owned_by_rotation_id bigint REFERENCES rotation(id), -- nullable for future standalone timeperiods

    CONSTRAINT pk_timeperiod PRIMARY KEY (id)
);

CREATE TABLE timeperiod_entry (
    id bigserial,
    timeperiod_id bigint NOT NULL REFERENCES timeperiod(id),
    rotation_member_id bigint REFERENCES rotation_member(id), -- nullable for future standalone timeperiods
    start_time bigint NOT NULL,
    end_time bigint NOT NULL,
    -- Is needed by icinga-notifications-web to prefilter entries, which matches until this time and should be ignored by the daemon.
    until_time bigint,
    timezone text NOT NULL, -- e.g. 'Europe/Berlin', relevant for evaluating rrule (DST changes differ between zones)
    rrule text, -- recurrence rule (RFC5545)

    CONSTRAINT pk_timeperiod_entry PRIMARY KEY (id)
);

CREATE TABLE rotation_member (
    id bigserial,
    rotation_id bigint NOT NULL REFERENCES rotation(id),
    contact_id bigint REFERENCES contact(id),
    contactgroup_id bigint REFERENCES contactgroup(id),
    position integer NOT NULL,

    UNIQUE (rotation_id, position), -- each position in a rotation can only be used once

    -- Two UNIQUE constraints prevent duplicate memberships of the same contact or contactgroup in a single rotation.
    -- Multiple NULLs are not considered to be duplicates, so rows with a contact_id but no contactgroup_id are
    -- basically ignored in the UNIQUE constraint over contactgroup_id and vice versa. The CHECK constraint below
    -- ensures that each row has only non-NULL values in one of these constraints.
    UNIQUE (rotation_id, contact_id),
    UNIQUE (rotation_id, contactgroup_id),
    CHECK (num_nonnulls(contact_id, contactgroup_id) = 1),

    CONSTRAINT pk_rotation_member PRIMARY KEY (id)
);

ALTER TABLE rule ADD COLUMN timeperiod_id bigint REFERENCES timeperiod(id);

DO $$ BEGIN
    DROP TYPE frequency_type;
EXCEPTION
    WHEN undefined_object THEN null;
END $$;
