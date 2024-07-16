# HTTP API

Icinga Notifications comes with its own HTTP API, [configurable](03-Configuration.md#http-api-configuration)
via `listen` and `debug-password`.

## Process Event

One possible source next to the Icinga 2 API is event submission to the Icinga Notifications HTTP API listener.
After creating a source with type _Other_ in Icinga Notifications Web,
the specified credentials can be used for HTTP Basic Authentication of a JSON-encoded
[Event](https://github.com/Icinga/icinga-notifications/blob/main/internal/event/event.go).

The authentication is performed via HTTP Basic Authentication, expecting `source-${id}` as the username,
`${id}` being the source's `id` within the database, and the configured password.

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
  "message": "Something went somewhere very wrong."
}
EOF
```

## Debugging Endpoints

There are multiple endpoints for dumping specific configurations.
To use those, the `debug-password` must be set and supplied via HTTP Basic Authentication next to an arbitrary username.

### Dump Config

The database-stored configuration from Icinga Notifications current viewpoint can be dumped as JSON.

```
curl -v -u ':debug-password' 'http://localhost:5680/dump-config'
```

### Dump Incidents

The current incidents can be dumped as JSON.

```
curl -v -u ':debug-password' 'http://localhost:5680/dump-incidents'
```

### Dump Schedules

All schedules with their assignee can be dumped in a human-readable form.

```
curl -v -u ':debug-password' 'http://localhost:5680/dump-schedules'
```
