CREATE TABLE contact (
    id bigserial PRIMARY KEY,
    full_name text,
    username text -- reference to web user
);

CREATE TABLE contact_address (
    id bigserial PRIMARY KEY,
    contact_id bigint REFERENCES contact(id),
    type text, -- 'phone', 'email', ...
    address text, -- phone number, email address, ...

    UNIQUE (contact_id, type) -- constraint may be relaxed in the future to support multiple addresses per type
);

CREATE TABLE contactgroup (
    id bigserial PRIMARY KEY,
    name text
);

CREATE TABLE contactgroup_member (
    contactgroup_id bigint NOT NULL REFERENCES contactgroup(id),
    contact_id bigint NOT NULL REFERENCES contact(id),

    PRIMARY KEY (contactgroup_id, contact_id)
);

CREATE TABLE timeperiod (
    id bigserial PRIMARY KEY,
    owned_by_schedule_id bigint REFERENCES timeperiod(id) -- nullable for future standalone timeperiods
);

CREATE TABLE timeperiod_entry (
    id bigserial PRIMARY KEY,
    timeperiod_id bigint REFERENCES timeperiod(id),
    start_time bigint,
    end_time bigint,
    timezone text, -- e.g. 'Europe/Berlin', relevant for evaluating rrule (DST changes differ between zones)
    rrule text, -- recurrence rule (RFC5545)
    description text
);

CREATE TABLE schedule (
    id bigserial PRIMARY KEY,
    name text
);

CREATE TABLE schedule_member (
    schedule_id bigint NOT NULL REFERENCES schedule(id),
    timeperiod_id bigint NOT NULL REFERENCES timeperiod(id),
    contact_id bigint REFERENCES contact(id),
    contactgroup_id bigint REFERENCES contactgroup(id),

    PRIMARY KEY (schedule_id, timeperiod_id, contact_id, contactgroup_id),
    CHECK (num_nonnulls(contact_id, contactgroup_id) = 1)
);
