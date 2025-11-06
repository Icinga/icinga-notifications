# HTTP API

Icinga Notifications comes with its own HTTP API, [configurable](03-Configuration.md#http-api-configuration)
via `listen` and `debug-password`.

## Process Event

Events can be submitted to Icinga Notifications using the `/process-event` HTTP API endpoint.

After creating a source in Icinga Notifications Web,
the specified credentials can be used via HTTP Basic Authentication to submit a JSON-encoded
[`Event`](https://github.com/Icinga/icinga-go-library/blob/main/notifications/event/event.go).

The authentication is performed via HTTP Basic Authentication, expecting `source-${id}` as the username,
`${id}` being the source's `id` within the database, and the configured password.

Events sent to Icinga Notifications are expected to match rules that describe further event escalations.
These rules can be created in the web interface.
Next to an array of `rule_ids`, a `rules_version` must be provided to ensure that the source has no outdated state.

When the submitted `rules_version` is either outdated or empty, the `/process-event` endpoint returns an HTTP 412 response.
The response's body is a JSON-encoded version of the
[`RulesInfo`](https://github.com/Icinga/icinga-go-library/blob/main/notifications/source/client.go),
containing the latest `rules_version` together with all rules for this source.
After reevaluating these rules, one can resubmit the event with the updated `rules_version`.

```
curl -v -u 'source-2:insecureinsecure' -d '@-' 'http://localhost:5680/process-event' <<EOF
{
  "name": "dummy-809: random fortune",
  "url": "http://localhost/icingaweb2/icingadb/service?name=random%20fortune&host.name=dummy-809",
  "tags": {
    "host": "dummy-809",
    "service": "random fortune"
  },
  "extra_tags": {
    "hostgroup/app-container": null,
    "hostgroup/department-dev": null,
    "hostgroup/env-qa": null,
    "hostgroup/location-rome": null,
    "servicegroup/app-mail": null,
    "servicegroup/department-nms": null,
    "servicegroup/env-prod": null,
    "servicegroup/location-berlin": null
  },
  "type": "state",
  "severity": "crit",
  "username": "",
  "message": "Something went somewhere very wrong.",
  "rules_version": "23",
  "rule_ids": ["0"]
}
EOF
```

## Debugging Endpoints

There are multiple endpoints for dumping specific configurations.
All of them are prefixed by `/debug`.
To use those, the `debug-password` must be set and supplied via HTTP Basic Authentication next to an arbitrary username.

### Dump Config

The database-stored configuration from Icinga Notifications current viewpoint can be dumped as JSON.

```
curl -v -u ':debug-password' 'http://localhost:5680/debug/dump-config'
```

### Dump Incidents

The current incidents can be dumped as JSON.

```
curl -v -u ':debug-password' 'http://localhost:5680/debug/dump-incidents'
```


### Dump Rules

The current rules can be dumped as JSON.

```
curl -v -u ':debug-password' 'http://localhost:5680/debug/dump-rules'
```

### Dump Schedules

All schedules with their assignee can be dumped in a human-readable form.

```
curl -v -u ':debug-password' 'http://localhost:5680/debug/dump-schedules'
```
