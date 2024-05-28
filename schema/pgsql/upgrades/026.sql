-- IMPORTANT: This schema upgrade removes all schedule-related configuration as it was changed in an incompatible way!

CREATE TYPE rotation_type AS ENUM ( '24-7', 'partial', 'multi' );

CREATE TABLE rotation (
    id bigserial,
    schedule_id bigint NOT NULL REFERENCES schedule(id),
    priority integer NOT NULL,
    name text NOT NULL,
    mode rotation_type NOT NULL,
    options text NOT NULL,
    first_handoff date NOT NULL,
    actual_handoff bigint NOT NULL,
    UNIQUE (schedule_id, priority, first_handoff),
    CONSTRAINT pk_rotation PRIMARY KEY (id)
);

CREATE TABLE rotation_member (
    id bigserial,
    rotation_id bigint NOT NULL REFERENCES rotation(id),
    contact_id bigint REFERENCES contact(id),
    contactgroup_id bigint REFERENCES contactgroup(id),
    position integer NOT NULL,
    UNIQUE (rotation_id, position),
    UNIQUE (rotation_id, contact_id),
    UNIQUE (rotation_id, contactgroup_id),
    CHECK (num_nonnulls(contact_id, contactgroup_id) = 1),
    CONSTRAINT pk_rotation_member PRIMARY KEY (id)
);

DROP TABLE schedule_member;

DELETE FROM timeperiod_entry WHERE timeperiod_id IN (SELECT id FROM timeperiod WHERE owned_by_schedule_id IS NOT NULL);
DELETE FROM timeperiod WHERE owned_by_schedule_id IS NOT NULL;

ALTER TABLE timeperiod
    DROP COLUMN owned_by_schedule_id,
    ADD COLUMN owned_by_rotation_id bigint REFERENCES rotation(id);

ALTER TABLE timeperiod_entry
    DROP COLUMN frequency,
    DROP COLUMN description,
    ADD COLUMN rotation_member_id bigint REFERENCES rotation_member(id);

DROP TYPE frequency_type;
