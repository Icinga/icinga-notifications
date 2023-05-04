CREATE TYPE boolenum AS ENUM ( 'n', 'y' );
CREATE TYPE incident_history_event_type AS ENUM ( 'source_severity_changed', 'incident_severity_changed', 'recipient_role_changed', 'escalation_triggered', 'rule_matched', 'opened', 'closed', 'notified' );
CREATE TYPE frequency_type AS ENUM ( 'MINUTELY', 'HOURLY', 'DAILY', 'WEEKLY', 'MONTHLY', 'QUARTERLY', 'YEARLY' );

CREATE TABLE contact (
    id bigserial,
    full_name text NOT NULL,
    username text, -- reference to web user
    color varchar(7), -- hex color codes e.g #000000

    CONSTRAINT pk_contact PRIMARY KEY (id),
    UNIQUE (username)
);

CREATE TABLE contact_address (
    id bigserial,
    contact_id bigint NOT NULL REFERENCES contact(id),
    type text NOT NULL, -- 'phone', 'email', ...
    address text NOT NULL, -- phone number, email address, ...

    CONSTRAINT pk_contact_address PRIMARY KEY (id),
    UNIQUE (contact_id, type) -- constraint may be relaxed in the future to support multiple addresses per type
);

CREATE TABLE contactgroup (
    id bigserial,
    name text NOT NULL,
    color varchar(7), -- hex color codes e.g #000000

    CONSTRAINT pk_contactgroup PRIMARY KEY (id)
);

CREATE TABLE contactgroup_member (
    contactgroup_id bigint NOT NULL REFERENCES contactgroup(id),
    contact_id bigint NOT NULL REFERENCES contact(id),

    CONSTRAINT pk_contactgroup_member PRIMARY KEY (contactgroup_id, contact_id)
);

CREATE TABLE schedule (
    id bigserial,
    name text NOT NULL,

    CONSTRAINT pk_schedule PRIMARY KEY (id)
);

CREATE TABLE timeperiod (
    id bigserial,
    owned_by_schedule_id bigint REFERENCES schedule(id), -- nullable for future standalone timeperiods

    CONSTRAINT pk_timeperiod PRIMARY KEY (id)
);

CREATE TABLE timeperiod_entry (
    id bigserial,
    timeperiod_id bigint NOT NULL REFERENCES timeperiod(id),
    start_time bigint NOT NULL,
    end_time bigint NOT NULL,
    -- Is needed by noma-web to prefilter entries, which matches until this time and should be ignored by the daemon.
    until_time bigint,
    timezone text NOT NULL, -- e.g. 'Europe/Berlin', relevant for evaluating rrule (DST changes differ between zones)
    rrule text, -- recurrence rule (RFC5545)
    -- Contains the same frequency types as in the rrule string except the `QUARTERLY` one, which is only offered
    -- by web that is represented as `FREQ=MONTHLY;INTERVAL=3` in a RRule string. So, this should be also ignored
    -- by the daemon.
    frequency frequency_type,
    description text,

    CONSTRAINT pk_timeperiod_entry PRIMARY KEY (id)
);

CREATE TABLE schedule_member (
    schedule_id bigint NOT NULL REFERENCES schedule(id),
    timeperiod_id bigint NOT NULL REFERENCES timeperiod(id),
    contact_id bigint REFERENCES contact(id),
    contactgroup_id bigint REFERENCES contactgroup(id),

    -- There is no PRIMARY KEY in that table as either contact_id or contactgroup_id should be allowed to be NULL.
    -- Instead, there are two UNIQUE constraints that prevent duplicate entries. Multiple NULLs are not considered to
    -- be duplicates, so rows with a contact_id but no contactgroup_id are basically ignored in the UNIQUE constraint
    -- over contactgroup_id and vice versa. The CHECK constraint below ensures that each row has only non-NULL values
    -- in one of these constraints.
    UNIQUE (schedule_id, timeperiod_id, contact_id),
    UNIQUE (schedule_id, timeperiod_id, contactgroup_id),
    CHECK (num_nonnulls(contact_id, contactgroup_id) = 1)
);

CREATE TABLE channel (
    id bigserial,
    name text NOT NULL,
    type text NOT NULL, -- 'email', 'sms', ...
    config text, -- JSON with channel-specific attributes
    -- for now type determines the implementation, in the future, this will need a reference to a concrete
    -- implementation to allow multiple implementations of a sms channel for example, probably even user-provided ones

    CONSTRAINT pk_channel PRIMARY KEY (id)
);

CREATE TABLE source (
    id bigserial,
    type text NOT NULL,
    name text NOT NULL,
    -- will likely need a distinguishing value for multiple sources of the same type in the future, like for example
    -- the Icinga DB environment ID for Icinga 2 sources

    CONSTRAINT pk_source PRIMARY KEY (id)
);

CREATE TABLE object (
    id bytea NOT NULL, -- SHA256 of identifying tags
    -- this will probably become more flexible in the future
    host text NOT NULL,
    service text,

    CHECK (length(id) = 256/8),

    CONSTRAINT pk_object PRIMARY KEY (id)
);

CREATE TABLE source_object (
    object_id bytea NOT NULL REFERENCES object(id),
    source_id bigint NOT NULL REFERENCES source(id),
    name text NOT NULL,
    url text,

    CONSTRAINT pk_source_object PRIMARY KEY (object_id, source_id)
);


CREATE TABLE object_extra_tag (
    object_id bytea NOT NULL REFERENCES object(id),
    source_id bigint NOT NULL REFERENCES source(id),

    tag text NOT NULL,
    value text NOT NULL,

    CONSTRAINT pk_object_extra_tag PRIMARY KEY (object_id, source_id, tag),
    FOREIGN KEY (object_id, source_id) REFERENCES source_object(object_id, source_id)
);

CREATE TYPE severity AS ENUM ('ok', 'debug', 'info', 'notice', 'warning', 'err', 'crit', 'alert', 'emerg');

CREATE TABLE event (
    id bigserial,
    time bigint NOT NULL,
    source_id bigint NOT NULL REFERENCES source(id),
    object_id bytea NOT NULL REFERENCES object(id),
    type text NOT NULL,
    severity severity,
    message text,
    username text,

    CONSTRAINT pk_event PRIMARY KEY (id),
    FOREIGN KEY (object_id, source_id) REFERENCES source_object(object_id, source_id)
);

CREATE TABLE rule (
    id bigserial,
    name text NOT NULL,
    timeperiod_id bigint REFERENCES timeperiod(id),
    object_filter text,
    is_active boolenum NOT NULL DEFAULT 'y',

    CONSTRAINT pk_rule PRIMARY KEY (id)
);

CREATE TABLE rule_escalation (
    id bigserial,
    rule_id bigint NOT NULL REFERENCES rule(id),
    position integer NOT NULL,
    condition text,
    name text, -- if not set, recipients are used as a fallback for display purposes
    fallback_for bigint REFERENCES rule_escalation(id),

    CONSTRAINT pk_rule_escalation PRIMARY KEY (id),

    UNIQUE (rule_id, position),
    CHECK (NOT (condition IS NOT NULL AND fallback_for IS NOT NULL))
);

CREATE TABLE rule_escalation_recipient (
    id bigserial,
    rule_escalation_id bigint NOT NULL REFERENCES rule_escalation(id),
    contact_id bigint REFERENCES contact(id),
    contactgroup_id bigint REFERENCES contactgroup(id),
    schedule_id bigint REFERENCES schedule(id),
    channel_type text NOT NULL,

    CONSTRAINT pk_rule_escalation_recipient PRIMARY KEY (id),

    CHECK (num_nonnulls(contact_id, contactgroup_id, schedule_id) = 1)
);

CREATE TABLE incident (
    id bigserial,
    object_id bytea NOT NULL REFERENCES object(id),
    started_at bigint NOT NULL,
    recovered_at bigint,
    severity severity NOT NULL,

    CONSTRAINT pk_incident PRIMARY KEY (id)
);

CREATE TABLE incident_event (
    incident_id bigint NOT NULL REFERENCES incident(id),
    event_id bigint NOT NULL REFERENCES event(id),

    CONSTRAINT pk_incident_event PRIMARY KEY (incident_id, event_id)
);

CREATE TYPE incident_contact_role AS ENUM ('recipient', 'subscriber', 'manager');

CREATE TABLE incident_contact (
    incident_id bigint NOT NULL REFERENCES incident(id),
    contact_id bigint REFERENCES contact(id),
    contactgroup_id bigint REFERENCES contactgroup(id),
    schedule_id bigint REFERENCES schedule(id),
    role incident_contact_role NOT NULL,

    CONSTRAINT key_incident_contact_contact UNIQUE (incident_id, contact_id),
    CONSTRAINT key_incident_contact_contactgroup UNIQUE (incident_id, contactgroup_id),
    CONSTRAINT key_incident_contact_schedule UNIQUE (incident_id, schedule_id),
    CONSTRAINT nonnulls_incident_recipients_check CHECK (num_nonnulls(contact_id, contactgroup_id, schedule_id) = 1)
);

CREATE TABLE incident_rule (
    incident_id bigint NOT NULL REFERENCES incident(id),
    rule_id bigint NOT NULL REFERENCES rule(id),

    CONSTRAINT pk_incident_rule PRIMARY KEY (incident_id, rule_id)
);

CREATE TABLE incident_rule_escalation_state (
    incident_id bigint NOT NULL REFERENCES incident(id),
    rule_escalation_id bigint NOT NULL REFERENCES rule_escalation(id),
    triggered_at bigint NOT NULL,

    CONSTRAINT pk_incident_rule_escalation_state PRIMARY KEY (incident_id, rule_escalation_id)
);

CREATE TABLE incident_history (
    id bigserial,
    incident_id bigint NOT NULL REFERENCES incident(id),
    rule_escalation_id bigint REFERENCES rule_escalation(id),
    event_id bigint REFERENCES event(id),
    contact_id bigint REFERENCES contact(id),
    contactgroup_id bigint REFERENCES contactgroup(id),
    schedule_id bigint REFERENCES schedule(id),
    rule_id bigint REFERENCES rule(id),
    caused_by_incident_history_id bigint REFERENCES incident_history(id),
    channel_type text,
    time bigint NOT NULL,
    message text,
    type incident_history_event_type NOT NULL,
    new_severity severity,
    old_severity severity,
    new_recipient_role incident_contact_role,
    old_recipient_role incident_contact_role,

    CONSTRAINT pk_incident_history PRIMARY KEY (id),
    FOREIGN KEY (incident_id, rule_escalation_id) REFERENCES incident_rule_escalation_state(incident_id, rule_escalation_id)
);
