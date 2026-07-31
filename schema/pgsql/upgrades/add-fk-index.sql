CREATE INDEX idx_object_id_tag_object_id ON object_id_tag(object_id);
CREATE INDEX idx_incident_object_id ON incident(object_id);
CREATE INDEX idx_incident_contact_incident_id ON incident_contact(incident_id);
CREATE INDEX idx_incident_rule_incident_id ON incident_rule(incident_id);
CREATE INDEX idx_incident_rule_escalation_state_incident_id ON incident_rule_escalation_state(incident_id);
CREATE INDEX idx_incident_history_incident_id ON incident_history(incident_id);
