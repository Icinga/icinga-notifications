# HTTP API

Icinga Notifications exposes an HTTP API for submitting events, retrieving and modifying incidents, and more.
Please refer to the [Configuration](03-Configuration.md#http-api-configuration) section for details on how to
configure the HTTP listener and the supported transports.

## Authentication

Icinga Notifications identifies the source of an HTTP API request differently depending on the transport used to
reach the HTTP API:

- **TCP:** HTTP Basic Authentication is used; both the source's username and password must match the configured
  credentials.
- **TCP with TLS:** The source is identified by the Subject of a TLS client certificate signed by the CA, instead of
  HTTP Basic Authentication. To submit this way, pass `--cacert ca.crt --cert client.crt --key client.key` to curl
  and omit `-u`. If no matching source is found, the request is rejected.
- **Unix socket:** The caller is identified automatically by their OS user; no HTTP Basic Auth or password is
  involved. To submit this way, pass `--unix-socket /run/icinga/icinga-notifications.sock` to curl and omit `-u`.
  curl must be executed as a user which is configured as the `listener_username` of a source.

!!! important

    A process connecting via the Unix socket can only submit events for sources whose configured
    listener_username matches the process's OS username. Restrict socket access using `socket_mode` and
    `socket_group` to limit which OS users can connect.

These rules apply to the [Process Event](#process-event), [Incidents](#incidents), and
[Notification History](#notification-history) endpoints. The [debug endpoints](#debugging-endpoints) use a separate,
transport-independent authentication scheme: the `debug-password` must be supplied via HTTP Basic Authentication next
to an arbitrary username, regardless of transport.

## Process Event

Events can be submitted to Icinga Notifications using the `/process-event` HTTP API endpoint.

After creating a source in Icinga Notifications Web,
the specified credentials can be used via HTTP Basic Authentication to submit a JSON-encoded
[`Event`](https://github.com/Icinga/icinga-go-library/blob/main/notifications/event/event.go).
See [Authentication](#authentication) for how sources are identified across the supported transports.

The `url` field of an event is optional, but if set, it must be an absolute URL such as
`https://example.com/icingaweb2/icingadb/host?name=example.com`. It is meant to point the notified contact at the
object in the web interface of the source that submitted the event. Icinga Notifications does not know where that
interface lives and therefore cannot complete a relative reference, so events carrying one are rejected with a
`400 Bad Request` status code.

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

## Response Format

All responses from the Icinga Notifications HTTP API endpoints are JSON-encoded and follow a consistent structure.
The only exception to this is the [`/process-event`](#process-event) endpoint, which returns a `204 No Content` status
code with an empty body in the successful case and a proper HTTP error status code in the error case.

The general structure of the response for all other endpoints consists of the following attributes:

| Attribute | Description                                                                   |
|-----------|-------------------------------------------------------------------------------|
| status    | A string indicating the overall status of the response object being streamed. |
| result    | A JSON object containing the result of the request.                           |

The `status` attribute can be a `success` or `error` string, indicating that the response result is either a successful
or failed response. The `result` attribute contains the actual result and varies depending on the request type and also
endpoint being used. For detailed information on the structure of the `result` attribute for each endpoint, please
refer to the respective sections below.

All responses are streamed as a series of JSON objects, one per line, in [JSON Lines/NDJSON](https://jsonlines.org/)
format. This allows for efficient processing of large datasets without requiring the entire response to be loaded into
memory at once. Each line in the response represents a single JSON object, and can be read and processed independently.
As a consequence, the general HTTP status code for those endpoints will always be 202 in the successful case, even if
the response contains no results at all. If some error occurs mid-stream after Icinga Notifications has already started
streaming the response, it will send a final JSON object with the `status` attribute set to `error` and the `result`
attribute containing a JSON object that describes the error.

## Incidents

The `/incidents` Icinga Notifications HTTP API endpoint allows sources to query and modify a list of open incidents.
Incidents can be retrieved by sending a `GET` request to the endpoint, and can be modified by sending a `POST` request
with the desired changes. The endpoint requires a `filter` query parameter to specify which incidents the source wants
to retrieve or modify. Please refer to the [API Filtering](#api-filtering) section for more details on how to construct
the filter.

See [Authentication](#authentication) for the transport-specific rules that apply here.

### Getting Incidents

In order to retrieve incidents, one can send a `GET` request to the `/incidents` endpoint with the appropriate `filter`
query parameter. In successful cases, the response result as described in the [Response Format](#response-format)
section will contain the following attributes for each incident that matches the filter:

| Attribute   | Description                                                                                   |
|-------------|-----------------------------------------------------------------------------------------------|
| is_muted    | A boolean indicating whether the incident is muted or not.                                    |
| object_tags | A dictionary containing the object ID tags associated with the incident.                      |
| severity    | The severity level of the incident (e.g., `crit`, `err`, `warning`, etc.).                    |

For error cases, the response result will contain the following attributes:

| Attribute | Description                                                                 |
|-----------|-----------------------------------------------------------------------------|
| error     | A string describing the error that occurred while retrieving the incidents. |

For instance, when using Icinga DB as a source, the `environment` object ID tag can be used to filter incidents for a
specific Icinga DB environment. The following example shows how to retrieve all incidents for the
`08434a503ec43bb67cd380c5d0b6217a1ebf924b` environment:

```
$ curl -u 'example:insecureinsecure' 'http://localhost:5680/incidents' -G --data-urlencode 'filter={"environment":"08434a503ec43bb67cd380c5d0b6217a1ebf924b"}'
...
{"status":"success","result":{"is_muted":false,"object_tags":{"environment":"08434a503ec43bb67cd380c5d0b6217a1ebf924b","host":"mailserver","service":"filesystem"},"severity":"crit"}}
{"status":"success","result":{"is_muted":true,"object_tags":{"environment":"08434a503ec43bb67cd380c5d0b6217a1ebf924b","host":"database","service":"load"},"severity":"err"}}
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

The [response result](#response-format) of this endpoint will contain the following attributes for each incident that
matches the filter. If the modification was successful, the `error` attribute will be omitted entirely. Otherwise, the
`error` attribute will be present and contain a string describing the error. Also, in that case, the response status
will be set to `error` instead of `success`.

| Attribute   | Description                                                                                  |
|-------------|----------------------------------------------------------------------------------------------|
| object_tags | A dictionary containing the object ID tags associated with the incident.                     |
| error       | An optional attribute that may be present if an error occurred while modifying the incident. |

The following example shows how to close the incident for the `mailserver` host and `filesystem` service in the
`08434a503ec43bb67cd380c5d0b6217a1ebf924b` environment:

```
$ curl -u 'example:insecureinsecure' -X POST 'http://localhost:5680/incidents' -G --data-urlencode 'filter={"environment":"08434a503ec43bb67cd380c5d0b6217a1ebf924b","host":"mailserver","service":"filesystem"}' -d '@-' <<EOF
{
  "close": true
}
EOF
```

Instead of closing an incident, one can also update its message by sending a `POST` request with the `message`
attribute set to the new message. For instance, the following example shows how to update the message of the incident
for the `database` host and `load` service in the `08434a503ec43bb67cd380c5d0b6217a1ebf924b` environment:

```
$ curl -u 'example:insecureinsecure' -X POST 'http://localhost:5680/incidents' -G --data-urlencode 'filter={"environment":"08434a503ec43bb67cd380c5d0b6217a1ebf924b","host":"database","service":"load"}' -d '@-' <<EOF
{
  "message": "The load on the database server has returned to normal."
}
EOF
```

When bulk modifying incidents, the changes will be applied to all the matching incidents that satisfy the filter
sequentially. If any of the incidents cannot be modified due to some reason, each incident will convey its own
status in the response as described in the [Response Format](#response-format) section. The following snippet shows
the result of the above curl request, where the incident for the `mailserver` host and `filesystem` service was
successfully modified, while the incident for the `database` host and `load` service failed to be modified due to
some server-side error.

```
...
{"status":"success","result":{"object_tags":{"environment":"08434a503ec43bb67cd380c5d0b6217a1ebf924b","host":"mailserver","service":"filesystem"}}}
{"status":"error","result":{"object_tags":{"environment":"08434a503ec43bb67cd380c5d0b6217a1ebf924b","host":"database","service":"load"},"error":"failed to modify incident, see server logs for details"}}
```

## Notification History

See [Authentication](#authentication) for the transport-specific rules that apply here.

In order to retrieve notification history entries, send a `GET` request to the `/notification-history` endpoint
with the query parameters `filter` described in [API Filtering](#api-filtering) and `since` set to a Unix timestamp in
milliseconds. Only entries whose `triggered_at` is greater than or equal to this value are returned.

In the successful case with matching notification history entries, the [response result](#response-format) will contain
the following attributes for each notification history entry:

| Attribute         | Description                                                                                |
|-------------------|--------------------------------------------------------------------------------------------|
| event_id          | Hex-encoded ID of the event that caused the notification to be triggered.                  |
| triggered_at      | Unix timestamp in milliseconds at which the notification attempt was made.                 |
| contact_name      | Full name of the contact the notification was sent to.                                     |
| contactgroup_name | Name of the contact group the contact was resolved from, if any.                           |
| schedule_name     | Name of the on-call schedule the contact was resolved from, if any.                        |
| channel_name      | Name of the channel used to deliver the notification.                                      |
| event_message     | The message of the event that triggered the notification.                                  |
| state             | The state of the notification attempt, either `sent` or `failed`.                          |

In error cases, the response result will contain the following attributes:

| Attribute | Description                                                                                    |
|-----------|------------------------------------------------------------------------------------------------|
| error     | A string describing the error that occurred while retrieving the notification history entries. |

When this happens mid-stream, the above attributes will be sent in a final JSON object, and the
[response status](#response-format) will be set to `error` instead of `success`. Afterward, the stream will be closed
and no further entries will be sent.

The following example shows how to retrieve all notification history entries recorded since
2026-01-01T00:00:00Z (`1767225600000`):

```
$ curl -u 'example:insecureinsecure' 'http://localhost:5680/notification-history?since=1767225600000' -G --data-urlencode 'filter={"host":"test-host"}'
...
{"status":"success","result":{"event_id":"b56665fc-70f1-48b9-a19c-b15beeb0152e","triggered_at":1788518901863,"contact_name":"Jane Doe","contactgroup_name":null,"schedule_name":"On-Call","channel_name":"email","event_message":"PING OK - Packet loss = 0%, RTA = 0.09 ms","state":"sent"}}
{"status":"success","result":{"event_id":"48bb1a43-4066-4b69-a14f-7d3d1fd76927","triggered_at":1788518901865,"contact_name":"Jane Doe","contactgroup_name":null,"schedule_name":"On-Call","channel_name":"email","event_message":"LOAD OK - total load average: 1.93, 0.98, 0.66","state":"sent"}}
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
To use those, the `debug-password` must be set and supplied via [HTTP Basic Authentication](#authentication) next to an arbitrary username.
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
