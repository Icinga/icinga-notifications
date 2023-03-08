ALTER TABLE contact ALTER COLUMN full_name SET NOT NULL;
ALTER TABLE contact_address ALTER COLUMN type SET NOT NULL;
ALTER TABLE contact_address ALTER COLUMN address SET NOT NULL;
ALTER TABLE contactgroup ALTER COLUMN name SET NOT NULL;
ALTER TABLE timeperiod_entry ALTER COLUMN timeperiod_id SET NOT NULL;
ALTER TABLE timeperiod_entry ALTER COLUMN start_time SET NOT NULL;
ALTER TABLE timeperiod_entry ALTER COLUMN end_time SET NOT NULL;
ALTER TABLE timeperiod_entry ALTER COLUMN timezone SET NOT NULL;
ALTER TABLE schedule ALTER COLUMN name SET NOT NULL;

CREATE TABLE channel (
    id bigserial PRIMARY KEY,
    name text NOT NULL,
    type text NOT NULL, -- 'email', 'sms', ...
    config text -- JSON with channel-specific attributes
    -- for now type determines the implementation, in the future, this will need a reference to a concrete
    -- implementation to allow multiple implementations of a sms channel for example, probably even user-provided ones
);

CREATE TABLE source (
    id bigserial PRIMARY KEY,
    type text NOT NULL,
    name text NOT NULL
    -- will likely need a distinguishing value for multiple sources of the same type in the future, like for example
    -- the Icinga DB environment ID for Icinga 2 sources
);

CREATE TABLE object (
    id bytea NOT NULL PRIMARY KEY, -- SHA256 of identifying tags
    -- this will probably become more flexible in the future
    host text NOT NULL,
    service text,

    CHECK (length(id) = 256/8)
);

CREATE TABLE source_object (
    object_id bytea NOT NULL REFERENCES object(id),
    source_id bigint NOT NULL REFERENCES source(id),
    name text NOT NULL,
    url text,

    PRIMARY KEY (object_id, source_id)
);


CREATE TABLE object_extra_tag (
    object_id bytea NOT NULL REFERENCES object(id),
    source_id bigint NOT NULL REFERENCES source(id),

    tag text NOT NULL,
    value text,

    PRIMARY KEY (object_id, source_id, tag),
    FOREIGN KEY (object_id, source_id) REFERENCES source_object(object_id, source_id)
);

CREATE TYPE severity AS ENUM ('ok', 'debug', 'info', 'notice', 'warning', 'err', 'crit', 'alert', 'emerg');

CREATE TABLE event (
    id bigserial PRIMARY KEY,
    time bigint NOT NULL,
    source_id bigint NOT NULL REFERENCES source(id),
    object_id bytea NOT NULL REFERENCES object(id),
    type text,
    severity severity,
    message text,
    username text,

    FOREIGN KEY (object_id, source_id) REFERENCES source_object(object_id, source_id)
);

CREATE TABLE rule (
    id bigserial PRIMARY KEY,
    name text NOT NULL,
    timeperiod_id bigint REFERENCES timeperiod(id),
    object_filter text
);

CREATE TABLE rule_escalation (
    id bigserial PRIMARY KEY,
    rule_id bigint NOT NULL REFERENCES rule(id),
    position integer NOT NULL,
    condition text,
    name text, -- if not set, recipients are used as a fallback for display purposes
    fallback_for bigint REFERENCES rule_escalation(id),

    UNIQUE (rule_id, position),
    CHECK (NOT (condition IS NOT NULL AND fallback_for IS NOT NULL))
);

CREATE TABLE rule_escalation_recipient (
    id bigserial PRIMARY KEY,
    rule_escalation_id bigint NOT NULL REFERENCES rule_escalation(id),
    contact_id bigint REFERENCES contact(id),
    contactgroup_id bigint REFERENCES contactgroup(id),
    schedule_id bigint REFERENCES schedule(id),
    channel_type text NOT NULL,

    CHECK (num_nonnulls(contact_id, contactgroup_id, schedule_id) = 1)
);

CREATE TABLE incident (
    id bigserial PRIMARY KEY,
    object_id bytea NOT NULL REFERENCES object(id),
    started_at bigint NOT NULL,
    recovered_at bigint,
    severity severity NOT NULL
);

CREATE TABLE incident_event (
    incident_id bigint NOT NULL REFERENCES incident(id),
    event_id bigint NOT NULL REFERENCES event(id),

    PRIMARY KEY (incident_id, event_id)
);

CREATE TYPE incident_contact_role AS ENUM ('recipient', 'subscriber', 'manager');

CREATE TABLE incident_contact (
    incident_id bigint NOT NULL REFERENCES incident(id),
    contact_id bigint NOT NULL REFERENCES contact(id),
    role incident_contact_role NOT NULL,

    PRIMARY KEY (incident_id, contact_id)
);

CREATE TABLE incident_rule (
    incident_id bigint NOT NULL REFERENCES incident(id),
    rule_id bigint NOT NULL REFERENCES rule(id),

    PRIMARY KEY (incident_id, rule_id)
);

CREATE TABLE incident_rule_escalation_state (
    incident_id bigint NOT NULL REFERENCES incident(id),
    rule_escalation_id bigint NOT NULL REFERENCES rule_escalation(id),
    triggered_at bigint NOT NULL,

    PRIMARY KEY (incident_id, rule_escalation_id)
);

CREATE TABLE incident_history (
    id bigserial PRIMARY KEY,
    incident_id bigint NOT NULL REFERENCES incident(id),
    rule_escalation_id bigint REFERENCES rule_escalation(id),
    time bigint NOT NULL,
    -- unstructured history log for very early versions, will become more structured in the future
    message text NOT NULL,

    FOREIGN KEY (incident_id, rule_escalation_id) REFERENCES incident_rule_escalation_state(incident_id, rule_escalation_id)
);
