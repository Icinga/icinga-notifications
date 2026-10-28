# HTTP API

Icinga Notifications exposes an HTTP API for submitting events, retrieving and modifying incidents, and more.
Please refer to the [Configuration](03-Configuration.md#http-api-configuration) section for details on how to
configure the HTTP listener and the supported transports.

## Versioning

All HTTP API endpoints, except for the [debug endpoints](#debugging-endpoints), are versioned and prefixed with the
API version, e.g. `/v1/process-event`. The current version of the HTTP API is `v1`.
Within a version, the API stays backwards compatible, i.e. existing endpoints, parameters and response fields are
neither removed nor changed in an incompatible way. Incompatible changes result in a new API version.

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

Events can be submitted to Icinga Notifications using the `/v1/process-event` HTTP API endpoint.

After creating a source in Icinga Notifications Web,
the specified credentials can be used via HTTP Basic Authentication to submit a JSON-encoded
[`Event`](#event-fields) with a `POST` request.
See [Authentication](#authentication) for how sources are identified across the supported transports.

### Event Fields

An event is a JSON object with the following fields. Fields that aren't listed here are rejected with a `400`, see
[Responses](#process-event-responses). The same type is available as
[`Event`](https://github.com/Icinga/icinga-go-library/blob/main/notifications/event/event.go) in the
icinga-go-library for sources written in Go, including its `Validate()` method, which implements the checks below.

| Field                | Type    | Description                                                                                                                                                                                                                                                                                                                                                                       |
|----------------------|---------|-----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `id`                 | string  | **Optional.** A globally unique UUID of the event. See [Event IDs](#event-ids).                                                                                                                                                                                                                                                                                                   |
| `name`               | string  | **Required.** The display name of the object the event is about. It isn't used to identify the object.                                                                                                                                                                                                                                                                            |
| `url`                | string  | **Optional.** An absolute URL pointing at the object in the source's web interface, passed on to the notified contacts. Relative URLs are rejected.                                                                                                                                                                                                                               |
| `tags`               | object  | **Required.** String key-value pairs that uniquely identify the object the event is about, e.g. `{"host": "example-809", "service": "disk /"}`. Events with identical tags refer to the same object and incident. At least one tag is required; keys and values must not be empty, and keys are limited to 255 characters.                                                        |
| `severity`           | string  | **Required, if `incident` is `true`.** The severity of the object, one of `ok`, `warning`, `error` or `critical`.                                                                                                                                                                                                                                                                 |
| `summary`            | string  | **Required.** A single-line description of the event, used where space is limited, e.g. email subjects. At most 1024 characters, without line breaks. The complete output belongs into `message`.                                                                                                                                                                                 |
| `message`            | string  | **Optional.** The complete human-readable message of the event, e.g. the check output.                                                                                                                                                                                                                                                                                            |
| `type`               | string  | **Required, if `incident` is `false`.** The kind of a notification-only event, e.g. `downtime_start`. The value is free-form and chosen by the source. Entries of notification rules and subscribers or managers of an incident can restrict the events they are notified about by it.                                                                                            |
| `muted`              | boolean | **Required, if `incident` is not set.** Whether the object is muted, e.g. due to a downtime or an acknowledgement. Muting silences the notifications of the object's incident from the next event on, unmuting lifts that, see [Rule Evaluation](#rule-evaluation). Can't be combined with `incident: false`, and `true` can't be combined with `close`. Requires `muted_reason`. |
| `muted_reason`       | string  | **Required, if `muted` is set.** The reason for (un)muting. It is recorded in the incident's history if the event changes the muted state. Ignored if `muted` is omitted.                                                                                                                                                                                                         |
| `incident`           | boolean | **Required, if `muted` is not set.** Selects what the event does, see [Event Kinds](#event-kinds) and [Rule Evaluation](#rule-evaluation). Either `true` or `false`; omit it for events which only change the muted state.                                                                                                                                                        |
| `close`              | boolean | **Optional.** Closes the incident of the object. Requires `incident: true`. Can only be `true` or omitted, not `false`.                                                                                                                                                                                                                                                           |
| `notify`             | boolean | **Optional.** Notifies the incident's recipients even if the severity didn't change, e.g. for a repeated problem state. Requires `incident: true` and can't be combined with `close`. Can only be `true` or omitted, not `false`.                                                                                                                                                 |
| `complete_relations` | array   | **Optional.** A list of relations in `relations` that are complete, see [Incomplete Relations](#incomplete-relations).                                                                                                                                                                                                                                                            |
| `relations`          | object  | **Optional.** Arbitrary additional information about the object, such as its host, services or groups, that the object filters of event rules are evaluated against. It isn't passed on to channels.                                                                                                                                                                              |

In addition to the constraints of the individual fields above (the tag and `summary` limits, an absolute `url`, and
`close` and `notify` being `true` or omitted), events violating any of the following rules are rejected with a `400`:

- `tags` and `summary` are always required, and `summary` must be a single line.
- `incident` and `muted` must not both be omitted.
- `severity` has to be set if and only if `incident` is `true`.
- `type` has to be set if and only if `incident` is `false`.
- `muted_reason` must not be empty if `muted` is set.
- `close` and `notify` require `incident: true` and are mutually exclusive.
- `muted` can't be combined with `incident: false`, and `muted: true` can't be combined with `close`.

#### Event Kinds

The combination of fields results in the following kinds of events:

| Kind           | Fields                                       | Effect                                                                                                                                                                                                                                              |
|----------------|----------------------------------------------|-----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| State change   | `incident: true`, `severity`                 | Opens an incident if there is none, otherwise it updates its severity, message and summary. Notifications are sent if the severity changed or an escalation triggered.                                                                              |
| Repeated state | `incident: true`, `severity`, `notify: true` | Like a state change, but recipients are notified even if the severity stays the same.                                                                                                                                                               |
| Recovery       | `incident: true`, `severity`, `close: true`  | Closes the incident, typically with severity `ok`. Without an open incident, one is opened and closed right away. Recipients are only notified if the severity changed or an escalation triggered, so closing at the current severity is silent.    |
| Notification   | `incident: false`, `type`                    | Notifies the recipients of matching notification rules and the open incident, if any. Nothing about the incident changes.                                                                                                                           |
| Mute change    | `muted`, `muted_reason`                      | Mutes or unmutes the open incident and always notifies its recipients, see below. Dropped without an open incident. Can be combined with `incident: true` to open or update an incident in a (un)muted state, or, with `muted: false`, to close it. |

The muted state is remembered for the incident. An event that doesn't carry `muted` neither mutes nor unmutes it, so a
source only has to set it if the state changes. A new incident starts unmuted unless the event opening it sets
`muted: true`.

An event with `muted` but without `incident` notifies the incident's recipients every time, even if the muted state
doesn't change: with an "Incident muted" or "Incident unmuted" notification if it does, otherwise with a regular update.
If the incident is already muted, these notifications are suppressed and only recorded in the history. Sources should
therefore only submit such events when the muted state actually changes.

#### Event IDs

The `id` is what lets Icinga Notifications tell a resubmitted event from a new one that looks the same. If a source
submits an event again, e.g. because it never received the response, the event is identified as duplicate by its `id`
and ignored. The duplicate is answered with `204` like any other event. Icinga Notifications remembers the `id` of a
successfully processed event for at least five minutes, up to ten, so only retries within that time are recognized.
The `id` of an event that failed to process is remembered for 24 to 48 hours, so retrying it with the same `id` is
ignored as well. Therefore, a source should generate a new UUID for every new event, but keep it for retries of that
event.

If `id` is omitted, Icinga Notifications derives one from the event's content and the submitting source. Identical
events without an `id` are thus deduplicated as well, even if they're meant to be distinct. Set an `id`, if the source
can submit identical events.

### Rule Evaluation

Events are evaluated against event rules configured in Icinga Notifications Web. There are two types of event rules,
and the event's `incident` field decides which of them, if any, are evaluated:

| `incident` | Rules evaluated    | Effect                                                                                                                                         |
|------------|--------------------|------------------------------------------------------------------------------------------------------------------------------------------------|
| `true`     | Escalation rules   | Opens a new incident or updates the open one, triggers escalations and notifies recipients.                                                    |
| `false`    | Notification rules | Notifies the recipients of matching notification rules right away. No incident is opened, closed or changed.                                   |
| omitted    | None               | Only updates the muted state of an already open incident and notifies its recipients about it. Without an open incident, the event is dropped. |

A rule's object filter is evaluated against the event's `relations`, so rules should be designed to match the
relations the source submits. Each entry of a notification rule may additionally restrict its recipients by the
event's `type`, e.g. `event_type=downtime_start`.

For `incident: false` events whose object already has an open incident, that incident's recipients are notified too,
according to the following rules:

- Recipients added by escalations are notified, unless the incident has a manager. Then only the manager and the
  subscribers are notified, see below.
- Subscribers and managers are only notified if the event's `type` is one of the event types they chose when
  subscribing or taking over the incident.

These notifications are recorded in the incident's history and carry the incident in the
[channel request](10-Channels.md#sendnotification). The incident's state itself stays unchanged.

Muting an incident only silences the notifications that belong to it. Its recipients aren't notified while it's
muted, and these notifications are recorded as suppressed in the incident's history. Contacts who are only notified
because of a notification rule are still notified, since those notifications aren't tied to the incident.

The mute only takes effect after the event that sets it has been processed. That event's own notifications are still
sent, including those of an incident opened with `muted: true`. Furthermore, recipients of an escalation that triggers
while the incident is muted are notified once, as the mute doesn't apply to them yet.

### Incomplete Relations

Only events with the `incident` field set are checked for missing relations. If such an event lacks relations that
the rules of the evaluated type require, Icinga Notifications responds with 422 if the
`X-Icinga-Reject-If-Relations-Incomplete` header is set to `true`. Without that header, the event is accepted anyway
if any of the following applies:

- the event has `incident: true`, i.e. it opens or escalates an incident,
- the event's object already has an open incident, or
- the source has at least one rule of the evaluated type without an object filter.

Otherwise, it's rejected with 422 as well. The response body lists the missing relations, so the source can
resubmit the event with them included:

```json
{"type": "attribute negotiation", "attributes": ["$[\"host\"][\"vars\"][\"foo\"]","$[\"services\"][*][\"vars\"][\"bar\"]"]}
```

Each entry in `attributes` is a missing relation, written as a normalized JSONPath expression. For example,
`$["host"]["vars"]["foo"]` is the `foo` custom variable of the host, and the same path can be expressed as
`host.vars.foo`. To mark such a relation as complete instead of sending it, add it or one of its parents
(e.g. `host.vars`) to `complete_relations`.

The `complete_relations` field of the event declares relations as complete, even if they don't contain everything
the rules refer to. For instance, if `complete_relations` contains `host.vars` and `services[*].vars`, the event
isn't rejected due to missing custom variables, because the source has stated that it has no further variables to send.

### Responses <a id="process-event-responses"></a>

The `/v1/process-event` endpoint returns the following HTTP status codes:

| Status | Meaning                                                                                                  |
|--------|----------------------------------------------------------------------------------------------------------|
| 204    | The event was accepted and queued for processing.                                                        |
| 400    | The body isn't valid JSON, contains unknown fields, or violates the [Event Fields](#event-fields) rules. |
| 401    | Authentication failed, see [Authentication](#authentication).                                            |
| 405    | The request method isn't `POST`.                                                                         |
| 422    | Required relations are missing, see [Incomplete Relations](#incomplete-relations).                       |
| 500    | The event couldn't be queued. Details are in the server log.                                             |

The `url` field of an event is optional. If set, it must be an absolute URL such as
`https://example.com/icingaweb2/icingadb/host?name=example.com`, pointing the notified contact at the object in the
source's web interface. Icinga Notifications doesn't know where that interface lives and can't resolve a relative
URL, so such events are rejected with `400`.

A `204` HTTP code means the event was accepted and successfully queued. The response body of a `400` is the plain text
validation error, e.g. `invalid event: at least one of 'incident' or 'muted' must be set`. Events that turn out to be
unprocessable later on are dropped and only logged.

### Example Request

An example request to submit a state change, which opens an incident or updates the open one, looks like this:

```
curl -v -u 'icingadb:insecureinsecure' -H 'X-Icinga-Reject-If-Relations-Incomplete: true' -d '@-' 'http://localhost:5680/v1/process-event' <<'EOF'
{
  "id": "6f1b3c0e-5a0e-4a53-9b8e-2f2d1c6d7a11",
  "incident": true,
  "name": "example-809: disk /",
  "url": "http://localhost/icingaweb2/icingadb/service?name=disk%20%2F&host.name=example-809",
  "tags": {
    "host": "example-809",
    "service": "disk /"
  },
  "severity": "critical",
  "summary": "[CRITICAL] - Filesystem checks failed",
  "message": "[CRITICAL] - Filesystem checks failed\n        \\_[CRITICAL] - / (ffs)\n                \\_[CRITICAL] - Free space absolute: 24 MiB (of 985 MiB)\n                \\_[CRITICAL] - Free space percentage: 2.436%\n                \\_[OK] - Inodes free: 97.4923% (151618 of 155518)|'/'=1007681536B;826918502.400000;930283315.200000;0;1033648128",
  "complete_relations": [
    "object",
    "host",
    "services",
    "hostgroups",
    "servicegroups"
  ],
  "relations": {
    "object": {
      "type": "service"
    },
    "host": {
      "name": "example-809",
      "display_name": "Example Host 809",
      "vars": {
        "os": "linux"
      }
    },
    "services": [
      {
        "name": "disk /",
        "display_name": "Disk /",
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

Submitting this exact request a second time within the [deduplication window](#event-ids) is answered with `204` as
well, but ignored, as the `id` is the same. Use a fresh UUID when trying it out repeatedly.

The following examples show the other [kinds of events](#event-kinds) for the same object. For brevity, they omit
the optional fields.

A recovery closes the incident again:

```
curl -u 'icingadb:insecureinsecure' -d '@-' 'http://localhost:5680/v1/process-event' <<EOF
{
  "incident": true,
  "name": "example-809: disk /",
  "close": true,
  "tags": {"host": "example-809", "service": "disk /"},
  "severity": "ok",
  "summary": "[OK] - Filesystem checks succeeded"
}
EOF
```

A notification-only event, here for the start of a downtime, notifies the recipients of matching notification rules
without touching the incident:

```
curl -u 'icingadb:insecureinsecure' -d '@-' 'http://localhost:5680/v1/process-event' <<EOF
{
  "incident": false,
  "name": "example-809: disk /",
  "type": "downtime_start",
  "tags": {"host": "example-809", "service": "disk /"},
  "summary": "Downtime started",
  "message": "Downtime for example-809: disk / started"
}
EOF
```

A mute change mutes the open incident. Sending `"muted": false` with a `muted_reason` unmutes it again:

```
curl -u 'icingadb:insecureinsecure' -d '@-' 'http://localhost:5680/v1/process-event' <<EOF
{
  "name": "example-809: disk /",
  "muted": true,
  "muted_reason": "Checkable is muted due to currently active downtime.",
  "tags": {"host": "example-809", "service": "disk /"},
  "summary": "Downtime started"
}
EOF
```

## Response Format

All responses from the Icinga Notifications HTTP API endpoints are JSON-encoded and follow a consistent structure.
The only exception to this is the [`/v1/process-event`](#process-event) endpoint, which returns a `204 No Content` status
code with an empty body in the successful case, see [Responses](#process-event-responses) and a proper HTTP error
status code in the error case.

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

The `/v1/incidents` Icinga Notifications HTTP API endpoint allows sources to query and modify a list of open incidents.
Incidents can be retrieved by sending a `GET` request to the endpoint, and can be modified by sending a `POST` request
with the desired changes. The endpoint requires a `filter` query parameter to specify which incidents the source wants
to retrieve or modify. Please refer to the [API Filtering](#api-filtering) section for more details on how to construct
the filter.

See [Authentication](#authentication) for the transport-specific rules that apply here.

### Getting Incidents

In order to retrieve incidents, one can send a `GET` request to the `/v1/incidents` endpoint with the appropriate `filter`
query parameter. In successful cases, the response result as described in the [Response Format](#response-format)
section will contain the following attributes for each incident that matches the filter:

| Attribute   | Description                                                                       |
|-------------|-----------------------------------------------------------------------------------|
| is_muted    | A boolean indicating whether the incident is muted or not.                        |
| object_tags | A dictionary containing the object ID tags associated with the incident.          |
| severity    | The severity level of the incident, i.e., `ok`, `warning`, `error` or `critical`. |

For error cases, the response result will contain the following attributes:

| Attribute | Description                                                                 |
|-----------|-----------------------------------------------------------------------------|
| error     | A string describing the error that occurred while retrieving the incidents. |

For instance, when using Icinga DB as a source, the `environment` object ID tag can be used to filter incidents for a
specific Icinga DB environment. The following example shows how to retrieve all incidents for the
`08434a503ec43bb67cd380c5d0b6217a1ebf924b` environment:

```
$ curl -u 'example:insecureinsecure' 'http://localhost:5680/v1/incidents' -G --data-urlencode 'filter={"environment":"08434a503ec43bb67cd380c5d0b6217a1ebf924b"}'
...
{"status":"success","result":{"is_muted":false,"object_tags":{"environment":"08434a503ec43bb67cd380c5d0b6217a1ebf924b","host":"mailserver","service":"filesystem"},"severity":"critical"}}
{"status":"success","result":{"is_muted":true,"object_tags":{"environment":"08434a503ec43bb67cd380c5d0b6217a1ebf924b","host":"database","service":"load"},"severity":"error"}}
```

### Modifying Incidents

Modifying incidents can be done by sending a `POST` request to the `/v1/incidents` endpoint with the appropriate
`filter` query parameter and a JSON-encoded body that describes the desired changes. The request body should be a JSON
object that contains the attributes to be modified, set to their new values. The list of attributes that currently can
be modified are listed in the following table.

| Attribute | Description                                                                        |
|-----------|------------------------------------------------------------------------------------|
| close     | Closes the incident matching the filter. This attribute can only be set to `true`. |
| message   | Updates the incident's message. This attribute can be set to any string value.     |

As opposed to the `/v1/process-event` endpoint, closing an incident this way will not have any side effects other than
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
$ curl -u 'example:insecureinsecure' -X POST 'http://localhost:5680/v1/incidents' -G --data-urlencode 'filter={"environment":"08434a503ec43bb67cd380c5d0b6217a1ebf924b","host":"mailserver","service":"filesystem"}' -d '@-' <<EOF
{
  "close": true
}
EOF
```

Instead of closing an incident, one can also update its message by sending a `POST` request with the `message`
attribute set to the new message. For instance, the following example shows how to update the message of the incident
for the `database` host and `load` service in the `08434a503ec43bb67cd380c5d0b6217a1ebf924b` environment:

```
$ curl -u 'example:insecureinsecure' -X POST 'http://localhost:5680/v1/incidents' -G --data-urlencode 'filter={"environment":"08434a503ec43bb67cd380c5d0b6217a1ebf924b","host":"database","service":"load"}' -d '@-' <<EOF
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

In order to retrieve notification history entries, send a `GET` request to the `/v1/notification-history` endpoint
with the query parameters `filter` described in [API Filtering](#api-filtering) and `since` set to a Unix timestamp in
milliseconds. Only entries whose `triggered_at` is greater than or equal to this value are returned. Failed notification
attempts are not included, i.e. the response only contains notifications that were successfully sent to a contact.

In the successful case with matching notification history entries, the [response result](#response-format) will contain
the following attributes for each notification history entry:

| Attribute         | Description                                                                                                 |
|-------------------|-------------------------------------------------------------------------------------------------------------|
| event_id          | Hex-encoded ID of the event that caused the notification to be triggered.                                   |
| triggered_at      | Unix timestamp in milliseconds at which the notification was sent.                                          |
| contact_name      | Full name of the contact the notification was sent to.                                                      |
| contactgroup_name | Name of the contact group the contact was resolved from, if any.                                            |
| schedule_name     | Name of the on-call schedule the contact was resolved from, if any.                                         |
| channel_name      | Name of the channel used to deliver the notification.                                                       |
| event_message     | The message of the event that triggered the notification.                                                   |
| event_summary     | The summary of the event that triggered the notification.                                                   |
| incident_closed   | A boolean indicating whether the associated incident was closed at the time the notification was triggered. |

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
$ curl -u 'example:insecureinsecure' 'http://localhost:5680/v1/notification-history?since=1767225600000' -G --data-urlencode 'filter={"host":"test-host"}'
...
{"status":"success","result":{"event_id":"b56665fc-70f1-48b9-a19c-b15beeb0152e","triggered_at":1788518901863,"contact_name":"Jane Doe","contactgroup_name":null,"schedule_name":"On-Call","channel_name":"email","event_message":"PING OK - Packet loss = 0%, RTA = 0.09 ms","event_summary":"PING OK - Packet loss = 0%, RTA = 0.09 ms","incident_closed":false}}
{"status":"success","result":{"event_id":"48bb1a43-4066-4b69-a14f-7d3d1fd76927","triggered_at":1788518901865,"contact_name":"Jane Doe","contactgroup_name":null,"schedule_name":"On-Call","channel_name":"email","event_message":"LOAD OK - total load average: 1.93, 0.98, 0.66","event_summary":"LOAD OK - total load average: 1.93, 0.98, 0.66","incident_closed":false}}
```

## API Filtering

Some of the Icinga Notifications API endpoints require to explicitly specify which objects the request should be
filtered for. For instance, the `/v1/incidents` endpoint requires a `filter` query parameter to specify which incidents
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
