# HTTP API

Icinga Notifications comes with its own HTTP API, [configurable](03-Configuration.md#http-api-configuration)
via `listen` and `debug-password`.

## Process Event

Events can be submitted to Icinga Notifications using the `/process-event` HTTP API endpoint.

After creating a source in Icinga Notifications Web,
the specified credentials can be used via HTTP Basic Authentication to submit a JSON-encoded
[`Event`](https://github.com/Icinga/icinga-go-library/blob/main/notifications/event/event.go).

Authentication differs by transport:

- **TCP:** HTTP Basic Authentication is used; both the source's username and password must match
  the configured credentials.
- **Unix socket:** The caller is identified automatically by their OS user. No HTTP Basic Auth or
  password is involved.

!!! important

    A process connecting via the Unix socket can only submit events for sources whose configured
    listener_username matches the process's OS username. Restrict socket access using `socket_mode` and
    `socket_group` to limit which OS users can connect.

!!! info

    Before Icinga Notifications version 0.2.0, the username was a fixed string based on the source ID, such as `source-${id}`.
    When upgrading a setup from an earlier version, these usernames are still valid, but can be changed in Icinga Notifications Web.

Events sent to Icinga Notifications are expected to match rules that describe further event escalations.
These rules can be configured in Icinga Notifications Web and should be designed to match the `relations` of the
submitted events. When submitting an event without the expected relations to evaluate the rules, Icinga Notifications
will reject the request with a `422 Unprocessable Entity` status code and a message describing the missing relations
when the `X-Icinga-Reject-If-Relations-Incomplete` header is set to `true`. Otherwise, the request will be accepted
nonetheless, when either there's an existing incident for the event's objects, the ongoing event causes a new incident
to be opened, or the source have at least one event rule without a configured object filter.

Furthermore, the `complete_relations` field of the event can be used to specify which relations or attributes of the
event should be considered as complete for the purpose of rule evaluation. For instance, if the `complete_relations`
field contains `host.vars` and `services[*].vars`, Icinga Notifications will not reject the event even if there are
rules that require custom variables that are not included in the event. This effectively tells Icinga Notifications
to ignore any missing custom variables because the source has explicitly declared that the event is complete and no
further information will be provided.

An example request to submit an event looks like this:

```
curl -v -u 'icingadb:insecureinsecure' -H 'X-Icinga-Reject-If-Relations-Incomplete: true' -d '@-' 'http://localhost:5680/process-event' <<EOF
{
  "name": "dummy-809: random fortune",
  "url": "http://localhost/icingaweb2/icingadb/service?name=random%20fortune&host.name=dummy-809",
  "tags": {
    "host": "dummy-809",
    "service": "random fortune"
  },
  "type": "state",
  "severity": "crit",
  "message": "Something went somewhere very wrong.",
  "complete_relations": [
    "host",
    "services",
    "hostgroups",
    "servicegroups"
  ],
  "relations": {
    "host": {
      "name": "dummy-809",
      "display_name": "My Dummy Host",
      "vars": {
        "os": "linux"
      }
    },
    "services": [
      {
        "name": "random fortune",
        "display_name": "Random Fortune Service",
        "vars": {
          "env": "production",
          "team": "devops"
        }
      }
    ],
    "hostgroups": [
      {
        "name": "linux-servers",
        "display_name": "Linux Servers"
      }
    ],
    "servicegroups": [
      {
        "name": "production-services",
        "display_name": "Production Services"
      }
    ]
  }
}
EOF
```

To submit over a Unix socket instead, pass `--unix-socket /run/icinga/icinga-notifications.sock` to curl.
No credentials are needed; the daemon identifies the caller by their OS user automatically.

!!! info

    curl must be executed as a user which is configured as listener_username of a source.

### Get Incidents

A source can query the list of open incidents belonging to its objects using the `/incidents` HTTP API endpoint.

Authentication follows the same transport-specific rules as for event submission: TCP requires HTTP
Basic Auth with username and password, while a Unix socket identifies the caller by their OS user.

```
$ curl -u 'example:insecureinsecure' 'http://localhost:5680/incidents'
[
  {
    "incident_id": "#23",
    "object_tags": {
      "host": "mailserver",
      "service": "filesystem"
    },
    "severity": "crit"
  },
  {
    "incident_id": "#42",
    "object_tags": {
      "host": "database",
      "service": "load"
    },
    "severity": "err"
  }
]
```

## Debugging Endpoints

There are multiple endpoints for dumping specific configurations.
All of them are prefixed by `/debug`.
To use those, the `debug-password` must be set and supplied via HTTP Basic Authentication next to an arbitrary username.
Unlike event submission, the debug endpoints always require the password regardless of the transport; connecting via
Unix socket does not bypass this check.

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
