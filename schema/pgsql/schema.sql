CREATE EXTENSION IF NOT EXISTS citext;

CREATE TYPE boolenum AS ENUM ( 'n', 'y' );
CREATE TYPE incident_history_event_type AS ENUM (
    -- Order to be honored for events with identical millisecond timestamps.
    'opened',
    'muted',
    'unmuted',
    'incident_severity_changed',
    'rule_matched',
    'escalation_triggered',
    'recipient_role_changed',
    'closed',
    'notified'
);
CREATE TYPE rotation_type AS ENUM ( '24-7', 'partial', 'multi' );
CREATE TYPE notification_state_type AS ENUM ( 'suppressed', 'pending', 'sent', 'failed' );

-- IPL ORM renders SQL queries with LIKE operators for all suggestions in the search bar,
-- which fails for numeric and enum types on PostgreSQL. Just like in Icinga DB Web.
CREATE OR REPLACE FUNCTION anynonarrayliketext(anynonarray, text)
    RETURNS bool
    LANGUAGE plpgsql
    IMMUTABLE
    PARALLEL SAFE
    AS $$
        BEGIN
            RETURN $1::TEXT LIKE $2;
        END;
    $$;
CREATE OPERATOR ~~ (LEFTARG=anynonarray, RIGHTARG=text, PROCEDURE=anynonarrayliketext);

CREATE TABLE available_channel_type (
    type text NOT NULL,
    name text NOT NULL,
    version text NOT NULL,
    author text NOT NULL,
    config_attrs text NOT NULL,

    CONSTRAINT pk_available_channel_type PRIMARY KEY (type)
);

CREATE TABLE channel (
    id bigserial,
    name citext NOT NULL,
    type text NOT NULL REFERENCES available_channel_type(type), -- 'email', 'sms', ...
    config text, -- JSON with channel-specific attributes
    -- for now type determines the implementation, in the future, this will need a reference to a concrete
    -- implementation to allow multiple implementations of a sms channel for example, probably even user-provided ones

    changed_at bigint NOT NULL,
    deleted boolenum NOT NULL DEFAULT 'n',

    CONSTRAINT pk_channel PRIMARY KEY (id)
);

CREATE INDEX idx_channel_changed_at ON channel(changed_at);

CREATE TABLE contact (
    id bigserial,
    full_name citext NOT NULL,
    username citext, -- reference to web user
    default_channel_id bigint NOT NULL REFERENCES channel(id),

    changed_at bigint NOT NULL,
    deleted boolenum NOT NULL DEFAULT 'n',

    CONSTRAINT pk_contact PRIMARY KEY (id),

    -- As the username is unique, it must be NULLed for deletion via "deleted = 'y'"
    UNIQUE (username)
);

CREATE INDEX idx_contact_changed_at ON contact(changed_at);

CREATE TABLE contact_address (
    id bigserial,
    contact_id bigint NOT NULL REFERENCES contact(id),
    type text NOT NULL, -- 'phone', 'email', ...
    address text NOT NULL, -- phone number, email address, ...

    changed_at bigint NOT NULL,
    deleted boolenum NOT NULL DEFAULT 'n',

    CONSTRAINT pk_contact_address PRIMARY KEY (id)
);

CREATE INDEX idx_contact_address_changed_at ON contact_address(changed_at);

CREATE TABLE contactgroup (
    id bigserial,
    name citext NOT NULL,

    changed_at bigint NOT NULL,
    deleted boolenum NOT NULL DEFAULT 'n',

    CONSTRAINT pk_contactgroup PRIMARY KEY (id)
);

CREATE INDEX idx_contactgroup_changed_at ON contactgroup(changed_at);

CREATE TABLE contactgroup_member (
    contactgroup_id bigint NOT NULL REFERENCES contactgroup(id),
    contact_id bigint NOT NULL REFERENCES contact(id),

    changed_at bigint NOT NULL,
    deleted boolenum NOT NULL DEFAULT 'n',

    CONSTRAINT pk_contactgroup_member PRIMARY KEY (contactgroup_id, contact_id)
);

CREATE INDEX idx_contactgroup_member_changed_at ON contactgroup_member(changed_at);

CREATE TABLE schedule (
    id bigserial,
    name citext NOT NULL,

    changed_at bigint NOT NULL,
    deleted boolenum NOT NULL DEFAULT 'n',

    CONSTRAINT pk_schedule PRIMARY KEY (id)
);

CREATE INDEX idx_schedule_changed_at ON schedule(changed_at);

CREATE TABLE rotation (
    id bigserial,
    schedule_id bigint NOT NULL REFERENCES schedule(id),
    -- the lower the more important, starting at 0, avoids the need to re-index upon addition
    priority integer,
    name text NOT NULL,
    mode rotation_type NOT NULL,
    -- JSON with rotation-specific attributes
    -- Needed exclusively by Web to simplify editing and visualisation
    options text NOT NULL,

    -- A date in the format 'YYYY-MM-DD' when the first handoff should happen.
    -- It is a string as handoffs are restricted to happen only once per day
    first_handoff date,

    -- Set to the actual time of the first handoff.
    -- If this is in the past during creation of the rotation, it is set to the creation time.
    -- Used by Web to avoid showing shifts that never happened
    actual_handoff bigint NOT NULL,

    changed_at bigint NOT NULL,
    deleted boolenum NOT NULL DEFAULT 'n',

    -- Each schedule can only have one rotation with a given priority starting at a given date.
    -- Columns schedule_id, priority, first_handoff must be NULLed for deletion via "deleted = 'y'".
    UNIQUE (schedule_id, priority, first_handoff),
    CHECK (deleted = 'y' OR priority IS NOT NULL AND first_handoff IS NOT NULL),

    CONSTRAINT pk_rotation PRIMARY KEY (id)
);

CREATE INDEX idx_rotation_changed_at ON rotation(changed_at);

CREATE TABLE timeperiod (
    id bigserial,
    owned_by_rotation_id bigint REFERENCES rotation(id), -- nullable for future standalone timeperiods

    changed_at bigint NOT NULL,
    deleted boolenum NOT NULL DEFAULT 'n',

    CONSTRAINT pk_timeperiod PRIMARY KEY (id)
);

CREATE INDEX idx_timeperiod_changed_at ON timeperiod(changed_at);

CREATE TABLE rotation_member (
    id bigserial,
    rotation_id bigint NOT NULL REFERENCES rotation(id),
    contact_id bigint REFERENCES contact(id),
    contactgroup_id bigint REFERENCES contactgroup(id),
    position integer,

    changed_at bigint NOT NULL,
    deleted boolenum NOT NULL DEFAULT 'n',

    -- Each position in a rotation can only be used once.
    -- Column position must be NULLed for deletion via "deleted = 'y'".
    UNIQUE (rotation_id, position),

    -- Two UNIQUE constraints prevent duplicate memberships of the same contact or contactgroup in a single rotation.
    -- Multiple NULLs are not considered to be duplicates, so rows with a contact_id but no contactgroup_id are
    -- basically ignored in the UNIQUE constraint over contactgroup_id and vice versa. The CHECK constraint below
    -- ensures that each row has only non-NULL values in one of these constraints.
    UNIQUE (rotation_id, contact_id),
    UNIQUE (rotation_id, contactgroup_id),

    CONSTRAINT ck_rotation_member_either_contact_id_or_contactgroup_id CHECK (num_nonnulls(contact_id, contactgroup_id) = 1),
    CONSTRAINT ck_rotation_member_non_deleted_needs_position CHECK (deleted = 'y' OR position IS NOT NULL),

    CONSTRAINT pk_rotation_member PRIMARY KEY (id)
);

CREATE INDEX idx_rotation_member_changed_at ON rotation_member(changed_at);

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

    changed_at bigint NOT NULL,
    deleted boolenum NOT NULL DEFAULT 'n',

    CONSTRAINT pk_timeperiod_entry PRIMARY KEY (id)
);

CREATE INDEX idx_timeperiod_entry_changed_at ON timeperiod_entry(changed_at);

CREATE TABLE source (
    id bigserial,
    -- The type "icinga2" is special and requires (at least some of) the icinga2_ prefixed columns.
    type text NOT NULL,
    name citext NOT NULL,
    -- will likely need a distinguishing value for multiple sources of the same type in the future, like for example
    -- the Icinga DB environment ID for Icinga 2 sources

    -- The column listener_password_hash is type-dependent.
    -- If type is not "icinga2", listener_password_hash is required to limit API access for incoming connections
    -- to the Listener. The username will be "source-${id}", allowing early verification.
    listener_password_hash text,

    -- Following columns are for the "icinga2" type.
    -- At least icinga2_base_url, icinga2_auth_user, and icinga2_auth_pass are required - see CHECK below.
    icinga2_base_url text,
    icinga2_auth_user text,
    icinga2_auth_pass text,
    -- icinga2_ca_pem specifies a custom CA to be used in the PEM format, if not NULL.
    icinga2_ca_pem text,
    -- icinga2_common_name requires Icinga 2's certificate to hold this Common Name if not NULL. This allows using a
    -- differing Common Name - maybe an Icinga 2 Endpoint object name - from the FQDN within icinga2_base_url.
    icinga2_common_name text,
    icinga2_insecure_tls boolenum NOT NULL DEFAULT 'n',

    changed_at bigint NOT NULL,
    deleted boolenum NOT NULL DEFAULT 'n',

    -- The hash is a PHP password_hash with PASSWORD_DEFAULT algorithm, defaulting to bcrypt. This check roughly ensures
    -- that listener_password_hash can only be populated with bcrypt hashes.
    -- https://icinga.com/docs/icinga-web/latest/doc/20-Advanced-Topics/#manual-user-creation-for-database-authentication-backend
    CHECK (listener_password_hash IS NULL OR listener_password_hash LIKE '$2y$%'),
    CHECK (type != 'icinga2' OR (icinga2_base_url IS NOT NULL AND icinga2_auth_user IS NOT NULL AND icinga2_auth_pass IS NOT NULL)),

    CONSTRAINT pk_source PRIMARY KEY (id)
);

CREATE INDEX idx_source_changed_at ON source(changed_at);

CREATE TABLE object (
    id bytea NOT NULL, -- SHA256 of identifying tags and the source.id
    source_id bigint NOT NULL REFERENCES source(id),
    name text NOT NULL,

    url text,
    -- mute_reason indicates whether an object is currently muted by its source, and its non-zero value is mapped to true.
    mute_reason text,

    CHECK (length(id) = 256/8),

    CONSTRAINT pk_object PRIMARY KEY (id)
);

CREATE TABLE object_id_tag (
    object_id bytea NOT NULL REFERENCES object(id),
    tag text NOT NULL,
    value text NOT NULL,

    CONSTRAINT pk_object_id_tag PRIMARY KEY (object_id, tag)
);

CREATE TABLE object_extra_tag (
    object_id bytea NOT NULL REFERENCES object(id),
    tag text NOT NULL,
    value text NOT NULL,

    CONSTRAINT pk_object_extra_tag PRIMARY KEY (object_id, tag)
);

CREATE TYPE event_type AS ENUM (
    'acknowledgement-cleared',
    'acknowledgement-set',
    'custom',
    'downtime-end',
    'downtime-removed',
    'downtime-start',
    'flapping-end',
    'flapping-start',
    'incident-age',
    'mute',
    'state',
    'unmute'
);
CREATE TYPE severity AS ENUM ('ok', 'debug', 'info', 'notice', 'warning', 'err', 'crit', 'alert', 'emerg');

CREATE TABLE event (
    id bigserial,
    time bigint NOT NULL,
    object_id bytea NOT NULL REFERENCES object(id),
    type event_type NOT NULL,
    severity severity,
    message text,
    username citext,
    mute boolenum,
    mute_reason text,

    CONSTRAINT pk_event PRIMARY KEY (id)
);

CREATE TABLE rule (
    id bigserial,
    name citext NOT NULL,
    timeperiod_id bigint REFERENCES timeperiod(id),
    object_filter text,

    changed_at bigint NOT NULL,
    deleted boolenum NOT NULL DEFAULT 'n',

    CONSTRAINT pk_rule PRIMARY KEY (id)
);

CREATE INDEX idx_rule_changed_at ON rule(changed_at);

CREATE TABLE rule_escalation (
    id bigserial,
    rule_id bigint NOT NULL REFERENCES rule(id),
    position integer,
    condition text,
    name citext, -- if not set, recipients are used as a fallback for display purposes
    fallback_for bigint REFERENCES rule_escalation(id),

    changed_at bigint NOT NULL,
    deleted boolenum NOT NULL DEFAULT 'n',

    CONSTRAINT pk_rule_escalation PRIMARY KEY (id),

    -- Each position in an escalation can only be used once.
    -- Column position must be NULLed for deletion via "deleted = 'y'"
    UNIQUE (rule_id, position),

    CONSTRAINT ck_rule_escalation_not_both_condition_and_fallback_for CHECK (NOT (condition IS NOT NULL AND fallback_for IS NOT NULL)),
    CONSTRAINT ck_rule_escalation_non_deleted_needs_position CHECK (deleted = 'y' OR position IS NOT NULL)
);

CREATE INDEX idx_rule_escalation_changed_at ON rule_escalation(changed_at);

CREATE TABLE rule_escalation_recipient (
    id bigserial,
    rule_escalation_id bigint NOT NULL REFERENCES rule_escalation(id),
    contact_id bigint REFERENCES contact(id),
    contactgroup_id bigint REFERENCES contactgroup(id),
    schedule_id bigint REFERENCES schedule(id),
    channel_id bigint REFERENCES channel(id),

    changed_at bigint NOT NULL,
    deleted boolenum NOT NULL DEFAULT 'n',

    CONSTRAINT pk_rule_escalation_recipient PRIMARY KEY (id),

    CHECK (num_nonnulls(contact_id, contactgroup_id, schedule_id) = 1)
);

CREATE INDEX idx_rule_escalation_recipient_changed_at ON rule_escalation_recipient(changed_at);

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
    channel_id bigint REFERENCES channel(id),
    time bigint NOT NULL,
    message text,
    type incident_history_event_type NOT NULL,
    new_severity severity,
    old_severity severity,
    new_recipient_role incident_contact_role,
    old_recipient_role incident_contact_role,
    notification_state notification_state_type,
    sent_at bigint,

    CONSTRAINT pk_incident_history PRIMARY KEY (id),
    FOREIGN KEY (incident_id, rule_escalation_id) REFERENCES incident_rule_escalation_state(incident_id, rule_escalation_id)
);

CREATE INDEX idx_incident_history_time_type ON incident_history(time, type);
COMMENT ON INDEX idx_incident_history_time_type IS 'Incident History ordered by time/type';

CREATE TABLE browser_session (
    php_session_id varchar(256) NOT NULL,
    username citext NOT NULL,
    user_agent varchar(4096) NOT NULL,
    authenticated_at bigint NOT NULL,

    CONSTRAINT pk_browser_session PRIMARY KEY (php_session_id)
);

CREATE INDEX idx_browser_session_authenticated_at ON browser_session (authenticated_at DESC);
CREATE INDEX idx_browser_session_username_agent ON browser_session (username, user_agent);
