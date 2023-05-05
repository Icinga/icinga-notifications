CREATE TABLE incident_source (
    incident_id bigint NOT NULL REFERENCES incident(id),
    source_id bigint NOT NULL REFERENCES source(id),
    severity severity NOT NULL,

    CONSTRAINT pk_incident_source PRIMARY KEY (incident_id, source_id)
);

INSERT INTO incident_source (incident_id, source_id, severity)
SELECT DISTINCT ON (incident_event.incident_id, event.source_id) incident_event.incident_id, event.source_id, event.severity
FROM incident_event JOIN event ON incident_event.event_id = event.id
WHERE event.type = 'state'
ORDER BY incident_event.incident_id, event.source_id, event.time DESC;
