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
    type text NOT NULL, -- 'email', 'sms', ...
    config text, -- JSON with channel-specific attributes
    -- for now type determines the implementation, in the future, this will need a reference to a concrete
    -- implementation to allow multiple implementations of a sms channel for example, probably even user-provided ones

    CONSTRAINT pk_channel PRIMARY KEY (id),
    CONSTRAINT fk_channel_available_channel_type FOREIGN KEY (type) REFERENCES available_channel_type(type)
);

CREATE TABLE contact (
    id bigserial,
    full_name citext NOT NULL,
    username citext, -- reference to web user
    default_channel_id bigint NOT NULL,

    CONSTRAINT pk_contact PRIMARY KEY (id),
    CONSTRAINT key_contact_username UNIQUE (username),
    CONSTRAINT fk_contact_channel FOREIGN KEY (default_channel_id) REFERENCES channel(id)
);

CREATE TABLE contact_address (
    id bigserial,
    contact_id bigint NOT NULL,
    type text NOT NULL, -- 'phone', 'email', ...
    address text NOT NULL, -- phone number, email address, ...

    CONSTRAINT pk_contact_address PRIMARY KEY (id),

    -- constraint may be relaxed in the future to support multiple addresses per type
    CONSTRAINT key_contact_address_contact_type UNIQUE (contact_id, type),

    CONSTRAINT fk_contact_address_contact FOREIGN KEY (contact_id) REFERENCES contact(id)
);

CREATE TABLE contactgroup (
    id bigserial,
    name citext NOT NULL,

    CONSTRAINT pk_contactgroup PRIMARY KEY (id)
);

CREATE TABLE contactgroup_member (
    contactgroup_id bigint NOT NULL,
    contact_id bigint NOT NULL,

    CONSTRAINT pk_contactgroup_member PRIMARY KEY (contactgroup_id, contact_id),
    CONSTRAINT fk_contactgroup_member_contactgroup FOREIGN KEY (contactgroup_id) REFERENCES contactgroup(id),
    CONSTRAINT fk_contactgroup_member_contact FOREIGN KEY (contact_id) REFERENCES contact(id)
);

CREATE TABLE schedule (
    id bigserial,
    name citext NOT NULL,

    CONSTRAINT pk_schedule PRIMARY KEY (id)
);

CREATE TABLE rotation (
    id bigserial,
    schedule_id bigint NOT NULL,
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

    -- Set to the actual time of the first handoff.
    -- If this is in the past during creation of the rotation, it is set to the creation time.
    -- Used by Web to avoid showing shifts that never happened
    actual_handoff bigint NOT NULL,

    CONSTRAINT pk_rotation PRIMARY KEY (id),

    -- each schedule can only have one rotation with a given priority starting at a given date
    CONSTRAINT key_rotation_schedule_schedule_priority_first_handoff UNIQUE (schedule_id, priority, first_handoff),

    CONSTRAINT fk_rotation_schedule FOREIGN KEY (schedule_id) REFERENCES schedule(id)
);

CREATE TABLE timeperiod (
    id bigserial,
    owned_by_rotation_id bigint, -- nullable for future standalone timeperiods

    CONSTRAINT pk_timeperiod PRIMARY KEY (id),
    CONSTRAINT fk_timeperiod_rotation FOREIGN KEY (owned_by_rotation_id) REFERENCES rotation(id)
);

CREATE TABLE rotation_member (
    id bigserial,
    rotation_id bigint NOT NULL,
    contact_id bigint,
    contactgroup_id bigint,
    position integer NOT NULL,

    -- each position in a rotation can only be used once
    CONSTRAINT key_rotation_member_rotation_position UNIQUE (rotation_id, position),

    -- Two UNIQUE constraints prevent duplicate memberships of the same contact or contactgroup in a single rotation.
    -- Multiple NULLs are not considered to be duplicates, so rows with a contact_id but no contactgroup_id are
    -- basically ignored in the UNIQUE constraint over contactgroup_id and vice versa. The CHECK constraint below
    -- ensures that each row has only non-NULL values in one of these constraints.
    CONSTRAINT key_rotation_member_rotation_contact UNIQUE (rotation_id, contact_id),
    CONSTRAINT key_rotation_member_rotation_contactgroup UNIQUE (rotation_id, contactgroup_id),
    CONSTRAINT nonnulls_rotation_member_recipients_check CHECK (num_nonnulls(contact_id, contactgroup_id) = 1),

    CONSTRAINT pk_rotation_member PRIMARY KEY (id),
    CONSTRAINT fk_rotation_member_rotation FOREIGN KEY (rotation_id) REFERENCES rotation(id),
    CONSTRAINT fk_rotation_member_contact FOREIGN KEY (contact_id) REFERENCES contact(id),
    CONSTRAINT fk_rotation_member_contactgroup FOREIGN KEY (contactgroup_id) REFERENCES contactgroup(id)
);

CREATE TABLE timeperiod_entry (
    id bigserial,
    timeperiod_id bigint NOT NULL,
    rotation_member_id bigint, -- nullable for future standalone timeperiods
    start_time bigint NOT NULL,
    end_time bigint NOT NULL,
    -- Is needed by icinga-notifications-web to prefilter entries, which matches until this time and should be ignored by the daemon.
    until_time bigint,
    timezone text NOT NULL, -- e.g. 'Europe/Berlin', relevant for evaluating rrule (DST changes differ between zones)
    rrule text, -- recurrence rule (RFC5545)

    CONSTRAINT pk_timeperiod_entry PRIMARY KEY (id),
    CONSTRAINT fk_timeperiod_entry_timeperiod FOREIGN KEY (timeperiod_id) REFERENCES timeperiod(id),
    CONSTRAINT fk_timeperiod_entry_rotation_member FOREIGN KEY (rotation_member_id) REFERENCES rotation_member(id)
);

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

    -- The hash is a PHP password_hash with PASSWORD_DEFAULT algorithm, defaulting to bcrypt. This check roughly ensures
    -- that listener_password_hash can only be populated with bcrypt hashes.
    -- https://icinga.com/docs/icinga-web/latest/doc/20-Advanced-Topics/#manual-user-creation-for-database-authentication-backend
    CONSTRAINT bcrypt_source_listener_password_hash_check CHECK (listener_password_hash IS NULL OR listener_password_hash LIKE '$2y$%'),
    CONSTRAINT nonnulls_source_icinga2_check CHECK (type != 'icinga2' OR (icinga2_base_url IS NOT NULL AND icinga2_auth_user IS NOT NULL AND icinga2_auth_pass IS NOT NULL)),

    CONSTRAINT pk_source PRIMARY KEY (id)
);

CREATE TABLE object (
    id bytea NOT NULL, -- SHA256 of identifying tags and the source.id
    source_id bigint NOT NULL,
    name text NOT NULL,

    url text,
    -- mute_reason indicates whether an object is currently muted by its source, and its non-zero value is mapped to true.
    mute_reason text,

    CONSTRAINT pk_object PRIMARY KEY (id),
    CONSTRAINT length_id32_check CHECK (length(id) = 256/8),
    CONSTRAINT fk_object_source FOREIGN KEY (source_id) REFERENCES source(id)
);

CREATE TABLE object_id_tag (
    object_id bytea NOT NULL,
    tag text NOT NULL,
    value text NOT NULL,

    CONSTRAINT pk_object_id_tag PRIMARY KEY (object_id, tag),
    CONSTRAINT fk_object_id_tag_object FOREIGN KEY (object_id) REFERENCES object(id)
);

CREATE TABLE object_extra_tag (
    object_id bytea NOT NULL,
    tag text NOT NULL,
    value text NOT NULL,

    CONSTRAINT pk_object_extra_tag PRIMARY KEY (object_id, tag),
    CONSTRAINT fk_object_extra_tag_object FOREIGN KEY (object_id) REFERENCES object(id)
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
    object_id bytea NOT NULL,
    type event_type NOT NULL,
    severity severity,
    message text,
    username citext,
    mute boolenum,
    mute_reason text,

    CONSTRAINT pk_event PRIMARY KEY (id),
    CONSTRAINT fk_event_object FOREIGN KEY (object_id) REFERENCES object(id)
);

CREATE TABLE rule (
    id bigserial,
    name citext NOT NULL,
    timeperiod_id bigint,
    object_filter text,
    is_active boolenum NOT NULL DEFAULT 'y',

    CONSTRAINT pk_rule PRIMARY KEY (id),
    CONSTRAINT fk_rule_timeperiod FOREIGN KEY (timeperiod_id) REFERENCES timeperiod(id)
);

CREATE TABLE rule_escalation (
    id bigserial,
    rule_id bigint NOT NULL,
    position integer NOT NULL,
    condition text,
    name citext, -- if not set, recipients are used as a fallback for display purposes
    fallback_for bigint,

    CONSTRAINT pk_rule_escalation PRIMARY KEY (id),
    CONSTRAINT key_rule_escalation_rule_position UNIQUE (rule_id, position),
    CONSTRAINT onenull_rule_escalation_condition_fallback_for_check CHECK (NOT (condition IS NOT NULL AND fallback_for IS NOT NULL)),
    CONSTRAINT fk_rule_escalation_rule FOREIGN KEY (rule_id) REFERENCES rule(id),
    CONSTRAINT fk_rule_escalation_rule_escalation FOREIGN KEY (fallback_for) REFERENCES rule_escalation(id)
);

CREATE TABLE rule_escalation_recipient (
    id bigserial,
    rule_escalation_id bigint NOT NULL,
    contact_id bigint,
    contactgroup_id bigint,
    schedule_id bigint,
    channel_id bigint,

    CONSTRAINT pk_rule_escalation_recipient PRIMARY KEY (id),
    CONSTRAINT nonnulls_rule_escalation_recipients_check CHECK (num_nonnulls(contact_id, contactgroup_id, schedule_id) = 1),
    CONSTRAINT fk_rule_escalation_recipient_rule_escalation FOREIGN KEY (rule_escalation_id) REFERENCES rule_escalation(id),
    CONSTRAINT fk_rule_escalation_recipient_contact FOREIGN KEY (contact_id) REFERENCES contact(id),
    CONSTRAINT fk_rule_escalation_recipient_contactgroup FOREIGN KEY (contactgroup_id) REFERENCES contactgroup(id),
    CONSTRAINT fk_rule_escalation_recipient_schedule FOREIGN KEY (schedule_id) REFERENCES schedule(id),
    CONSTRAINT fk_rule_escalation_recipient_channel FOREIGN KEY (channel_id) REFERENCES channel(id)
);

CREATE TABLE incident (
    id bigserial,
    object_id bytea NOT NULL,
    started_at bigint NOT NULL,
    recovered_at bigint,
    severity severity NOT NULL,

    CONSTRAINT pk_incident PRIMARY KEY (id),
    CONSTRAINT fk_incident_object FOREIGN KEY (object_id) REFERENCES object(id)
);

CREATE TABLE incident_event (
    incident_id bigint NOT NULL,
    event_id bigint NOT NULL,

    CONSTRAINT pk_incident_event PRIMARY KEY (incident_id, event_id),
    CONSTRAINT fk_incident_event_incident FOREIGN KEY (incident_id) REFERENCES incident(id),
    CONSTRAINT fk_incident_event_event FOREIGN KEY (event_id) REFERENCES event(id)
);

CREATE TYPE incident_contact_role AS ENUM ('recipient', 'subscriber', 'manager');

CREATE TABLE incident_contact (
    incident_id bigint NOT NULL,
    contact_id bigint,
    contactgroup_id bigint,
    schedule_id bigint,
    role incident_contact_role NOT NULL,

    CONSTRAINT key_incident_contact_contact UNIQUE (incident_id, contact_id),
    CONSTRAINT key_incident_contact_contactgroup UNIQUE (incident_id, contactgroup_id),
    CONSTRAINT key_incident_contact_schedule UNIQUE (incident_id, schedule_id),
    CONSTRAINT nonnulls_incident_recipients_check CHECK (num_nonnulls(contact_id, contactgroup_id, schedule_id) = 1),
    CONSTRAINT fk_incident_contact_incident FOREIGN KEY (incident_id) REFERENCES incident(id),
    CONSTRAINT fk_incident_contact_contact FOREIGN KEY (contact_id) REFERENCES contact(id),
    CONSTRAINT fk_incident_contact_contactgroup FOREIGN KEY (contactgroup_id) REFERENCES contactgroup(id),
    CONSTRAINT fk_incident_contact_schedule FOREIGN KEY (schedule_id) REFERENCES schedule(id)
);

CREATE TABLE incident_rule (
    incident_id bigint NOT NULL,
    rule_id bigint NOT NULL,

    CONSTRAINT pk_incident_rule PRIMARY KEY (incident_id, rule_id),
    CONSTRAINT fk_incident_rule_incident FOREIGN KEY (incident_id) REFERENCES incident(id),
    CONSTRAINT fk_incident_rule_rule FOREIGN KEY (rule_id) REFERENCES rule(id)
);

CREATE TABLE incident_rule_escalation_state (
    incident_id bigint NOT NULL,
    rule_escalation_id bigint NOT NULL,
    triggered_at bigint NOT NULL,

    CONSTRAINT pk_incident_rule_escalation_state PRIMARY KEY (incident_id, rule_escalation_id),
    CONSTRAINT fk_incident_rule_escalation_state_incident FOREIGN KEY (incident_id) REFERENCES incident(id),
    CONSTRAINT fk_incident_rule_escalation_state_rule_escalation FOREIGN KEY (rule_escalation_id) REFERENCES rule_escalation(id)
);

CREATE TABLE incident_history (
    id bigserial,
    incident_id bigint NOT NULL,
    rule_escalation_id bigint,
    event_id bigint,
    contact_id bigint,
    contactgroup_id bigint,
    schedule_id bigint,
    rule_id bigint,
    channel_id bigint,
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
    CONSTRAINT fk_incident_history_incident_rule_escalation_state FOREIGN KEY (incident_id, rule_escalation_id) REFERENCES incident_rule_escalation_state(incident_id, rule_escalation_id),
    CONSTRAINT fk_incident_history_incident FOREIGN KEY (incident_id) REFERENCES incident(id),
    CONSTRAINT fk_incident_history_rule_escalation FOREIGN KEY (rule_escalation_id) REFERENCES rule_escalation(id),
    CONSTRAINT fk_incident_history_event FOREIGN KEY (event_id) REFERENCES event(id),
    CONSTRAINT fk_incident_history_contact FOREIGN KEY (contact_id) REFERENCES contact(id),
    CONSTRAINT fk_incident_history_contactgroup FOREIGN KEY (contactgroup_id) REFERENCES contactgroup(id),
    CONSTRAINT fk_incident_history_schedule FOREIGN KEY (schedule_id) REFERENCES schedule(id),
    CONSTRAINT fk_incident_history_rule FOREIGN KEY (rule_id) REFERENCES rule(id),
    CONSTRAINT fk_incident_history_channel FOREIGN KEY (channel_id) REFERENCES channel(id)
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
