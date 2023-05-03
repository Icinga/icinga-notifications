CREATE TABLE incident_source (
    incident_id bigint NOT NULL REFERENCES incident(id),
    source_id bigint NOT NULL REFERENCES source(id),
    severity severity NOT NULL,

    CONSTRAINT pk_incident_source PRIMARY KEY (incident_id, source_id)
);

INSERT INTO incident_source (source_id, incident_id, severity) SELECT 1, id, severity FROM incident;
