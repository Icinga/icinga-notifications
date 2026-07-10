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

## Incidents

The `/incidents` Icinga Notifications HTTP API endpoint allows sources to query and modify a list of open incidents
belonging to its objects. Incidents can be retrieved by sending a `GET` request to the endpoint, and can be modified
by sending a `POST` request with the desired changes. The endpoint requires a `filter` query parameter to specify which
incidents the source wants to retrieve or modify. Please refer to the [API Filtering](#api-filtering) section for more
details on how to construct the filter.

Authentication follows the same transport-specific rules as for event submission: TCP requires HTTP
Basic Auth with username and password, while a Unix socket identifies the caller by their OS user.

### Getting Incidents

In order to retrieve incidents, one can send a `GET` request to the `/incidents` endpoint with the appropriate `filter`
query parameter. Currently, this endpoint will include the attributes listed below in the response for each incident:

| Attribute   | Description                                                                |
|-------------|----------------------------------------------------------------------------|
| is_muted    | A boolean indicating whether the incident is muted or not.                 |
| object_tags | A dictionary containing the object ID tags associated with the incident.   |
| severity    | The severity level of the incident (e.g., `crit`, `err`, `warning`, etc.). |

For instance, when using Icinga DB as a source, the `environment` object ID tag can be used to filter incidents for a
specific Icinga DB environment. The following example shows how to retrieve all incidents for the
`08434a503ec43bb67cd380c5d0b6217a1ebf924b` environment:

```
$ curl -u 'example:insecureinsecure' 'http://localhost:5680/incidents?filter=%7B%22environment%22%3A%2208434a503ec43bb67cd380c5d0b6217a1ebf924b%22%7D'
[
  {
    "is_muted": false,
    "object_tags": {
      "environment": "08434a503ec43bb67cd380c5d0b6217a1ebf924b",
      "host": "mailserver",
      "service": "filesystem"
    },
    "severity": "crit"
  },
  {
    "is_muted": true,
    "object_tags": {
      "environment": "08434a503ec43bb67cd380c5d0b6217a1ebf924b",
      "host": "database",
      "service": "load"
    },
    "severity": "err"
  }
]
```

### Modifying Incidents

Modifying incidents can be done by sending a `POST` request to the `/incidents` endpoint with the appropriate
`filter` query parameter and a JSON-encoded body that describes the desired changes. The request body should be a JSON
object that contains the attributes to be modified, set to their new values. The list of attributes that currently can
be modified are listed in the following table.

| Attribute | Description                                                                        |
|-----------|------------------------------------------------------------------------------------|
| close     | Closes the incident matching the filter. This attribute can only be set to `true`. |
| message   | Updates the incident's message. This attribute can be set to any string value.     |

As opposed to the `/process-event` endpoint, closing an incident this way will not have any side effects other than
marking the incident as closed. For instance, it will not trigger any notifications or trigger any escalations. Also,
note that the `close` attribute can only be set to `true`, otherwise Icinga Notifications will reject the request with
a 400 status code. The `message` attribute can be used to update the incident's message without any side effects, as
well. For instance, this can be useful to regularly synchronize only the plugin output of the associated object so that
the incident's message is always up to date.

The following example shows how to close the incident for the `mailserver` host and `filesystem` service in the
`08434a503ec43bb67cd380c5d0b6217a1ebf924b` environment:

The URL-encoded filter in the example below corresponds to the following JSON object:

```json
{
  "environment": "08434a503ec43bb67cd380c5d0b6217a1ebf924b",
  "host": "mailserver",
  "service": "filesystem"
}
```

```
$ curl -u 'example:insecureinsecure' -X POST 'http://localhost:5680/incidents?filter=%7B%22environment%22%3A%2208434a503ec43bb67cd380c5d0b6217a1ebf924b%22%2C%22host%22%3A%22mailserver%22%2C%22service%22%3A%22filesystem%22%7D' -d '@-' <<EOF
{
  "close": true
}
EOF
```

Instead of closing an incident, one can also update its message by sending a `POST` request with the `message`
attribute set to the new message. For instance, the following example shows how to update the message of the incident
for the `database` host and `load` service in the `08434a503ec43bb67cd380c5d0b6217a1ebf924b` environment:

```
$ curl -u 'example:insecureinsecure' -X POST 'http://localhost:5680/incidents?filter=%7B%22environment%22%3A%2208434a503ec43bb67cd380c5d0b6217a1ebf924b%22%2C%22host%22%3A%22database%22%2C%22service%22%3A%22load%22%7D' -d '@-' <<EOF
{
  "message": "The load on the database server has returned to normal."
}
EOF
```

When bulk modifying incidents, the changes will be applied to all the matching incidents that satisfy the filter
sequentially. If any of the incidents cannot be modified due to some reason, each incident will convey its own status
in the response, and the general HTTP status code will be the most severe one among all the incidents. For instance, if
one incident is successfully closed, but another one cannot, the response will contain a `200 OK` status code for the
first incident and a `500` status code for the second incident, and the overall HTTP status code will be
`500 Internal Server Error`. The complete response body looks like this:

```json
[
  {
    "object_tags": {
      "environment": "08434a503ec43bb67cd380c5d0b6217a1ebf924b",
      "host": "mailserver",
      "service": "filesystem"
    },
    "code": 200,
    "status": "incident modified successfully"
  },
  {
    "object_tags": {
      "environment": "08434a503ec43bb67cd380c5d0b6217a1ebf924b",
      "host": "database",
      "service": "load"
    },
    "code": 500,
    "status": "failed to modify incident, see server logs for details"
  }
]
```

## API Filtering

Some of the Icinga Notifications API endpoints require to explicitly specify which objects the request should be
filtered for. For instance, the `/incidents` endpoint requires a `filter` query parameter to specify which incidents
it should either retrieve or modify. The `filter` query parameter is a JSON-encoded object that describes the
filtering criteria. The filter syntax and semantics are as follows:

The `filter` parameter can either be a [JSON object](#json-object-filter) or a [JSON array](#json-array-filter) of objects.
The filter is evaluated against the object tags of the incidents, and only incidents that satisfy the specified criteria
are eligible for modification or retrieval. Please refer to the [JSON Object Filter](#json-object-filter) and
[JSON Array Filter](#json-array-filter) sections for more details on how they work.

### JSON Object Filter

Each object represents a set of filtering criteria that must be satisfied for an incident to be modified or included in
the response. All the key value pairs in the object are combined with a logical **AND**, meaning that all the specified
criteria must be met. In other words, for each key value pair in the JSON object filter, the incident's object tags must
contain the key and have the corresponding value for the filter to be satisfied.

For instance, given the following filter:

```json
{
  "host": "mailserver",
  "service": "filesystem"
}
```

The endpoint will return or modify only incidents that have the `host` tag set to `mailserver` **and** the `service`
tag set to `filesystem`. If either of the tags is missing or has a different value, the filter is not satisfied, and
the incident will not be included in the response. To express a **NOT EXISTS** condition, please use the `null` JSON
value. For example, given the following filter:

```json
{
  "host": "mailserver",
  "service": null
}
```

This filter will match incidents that have the `host` object ID tag set to `mailserver` and do **not** have a `service`
object ID tag at all. In other words, the `service` tag must not exist in the incident's object tags for the filter to
be satisfied when all other key value pairs in the filter are satisfied.

!!! important

    The **NOT EXISTS** condition can only be used in conjunction with other key value pairs in the filter. It cannot
    be used on its own, as this would result in an ambiguous filter that could potentially match all objects based
    on the absence of a single tag without any other postive criteria to narrow down the selection, posing a risk of
    unintended modifications or retrievals.

### JSON Array Filter

JSON array of [objects](#json-object-filter) can be used to express logical **OR** conditions. Each object in the
array represents a separate set of filtering criteria, and an incident is considered to match the filter if it
satisfies at least one of the objects in the array.

!!! info

    If the empty array `[]` is used as a filter, it will not match any incidents, and the endpoint will always return
    an empty list. In other words, the endpoint always requires at least one object in the array to match any incidents.

For example, given the following filter:

```json
[
  {
    "host": "mailserver",
    "service": "filesystem"
  },
  {
    "host": "database",
    "service": null
  }
]
```

This filter will match incidents that either have the `host` tag set to `mailserver` **and** the `service` tag set to
`filesystem`, **or** have the `host` tag set to `database` and do **not** have a `service` tag at all. In other words,
the filter will match incidents that satisfy either of the two sets of criteria, allowing for more complex filtering
scenarios. These filtering capabilities enable to precisely target the incidents one want to retrieve or modify based
on their object tags, providing flexibility and control over the incident management process. However, the filter
syntax does not support more complex logical expressions, such as nested conditions or combinations of `AND` and `OR`
within a single object. Any attempt to use such expressions will result in a `400 Bad Request` response, indicating 
that the filter is invalid.

When constructing filters, it is important to ensure that the filter is properly URL-encoded when included in the query
string of the HTTP request. This is necessary because certain characters in the JSON representation of the filter, such
as curly braces, quotes, and commas, have special meanings in URLs and must be encoded to avoid ambiguity. For example,
the filter `{"host":"mailserver","service":"filesystem"}` should be URL-encoded as
`%7B%22host%22%3A%22mailserver%22%2C%22service%22%3A%22filesystem%22%7D` when included in the query string.

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
