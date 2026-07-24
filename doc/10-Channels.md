# Channels

After Icinga Notifications decides to send a notification of any kind, it is passed to a channel plugin.
Such a plugin submits the notification event to a domain-specific channel, such as email or a chat client.

Icinga Notifications comes packed with channels, but also enables you to develop your own channels.

To make these channels available to Icinga Notifications, they must be placed in the
[channels directory](03-Configuration.md#channels-directory),
which is done automatically during package installations.
At startup, Icinga Notifications scans this directory, starts each channel once to query its configuration options
and stores those options in the database.
Using this information, Icinga Notifications Web allows channels to be configured,
which are then started, configured, and finally used to send notification events from Icinga Notifications.

## Technical Channel Description

Channel plugins are independent processes that run continuously, started and supervised by Icinga Notifications.
They receive [JSON-RPC 2.0](https://www.jsonrpc.org/specification) requests on `stdin` and reply with JSON-RPC 2.0
responses on `stdout`. For each request that includes an `id`, the channel must return a response, even if the request
was invalid or could not be processed. Requests without an `id` are JSON-RPC Notifications and must not be responded to.
Channel plugins can be written in any programming language, as long as they adhere to the communication protocol and
implement the required methods, which are described below.

### Architecture

The channel plugin architecture is designed to be simple and flexible, allowing for easy integration of new channels
into Icinga Notifications. The channel plugin acts as a server, receiving requests from Icinga Notifications and
sending back responses. The channel plugin can also send requests to Icinga Notifications, allowing for bidirectional
communication. Please refer to the [Available Icinga Notifications Methods](#available-icinga-notifications-methods)
section for more information on the methods available to channel plugins.

This documentation uses beautified JSON for ease of reading.

#### Request

Icinga Notifications sends requests to the channel plugin as JSON objects with the following fields, which are defined
in the [JSON-RPC 2.0 specification](https://www.jsonrpc.org/specification#request_object):

| Field   | Type             | Description                                  |
|---------|------------------|----------------------------------------------|
| jsonrpc | String           | **Required.** Must be `"2.0"`.               |
| method  | String           | **Required.** Request method to call.        |
| params  | JSON object      | **Optional.** Params for the request method. |
| id      | Unsigned integer | **Optional.** Unique identifier.             |

The `jsonrpc` field denotes the version of the JSON-RPC protocol used, which must be `"2.0"` for all requests and
responses. The `method` field specifies the name of the request method to call, which is one of the methods described
in the [RPC Methods](#rpc-methods) section for channel plugins. The `params` field contains the parameters for the
request method, which is a JSON object, and may be omitted entirely if the request method does not require any
parameters. Lastly, the `id` field is a unique identifier for the request, which is used to match the response to the
request, and will always be set by Icinga Notifications when sending requests to the channel plugin.

Examples:

- Simple request without any `params`:
  ```json
  {
    "jsonrpc": "2.0",
    "method": "ping",
    "id": 1000
  }
  ```
- Request with `params` of different types:
  ```json
  {
    "jsonrpc": "2.0",
    "method": "sleep",
    "params": {
      "duration": 30000000000,
      "message": "Sleeping for 30 seconds"
    },
    "id": 2000
  }
  ```

#### Response

Each request must be answered by the channel plugin with a JSON object containing the following fields, which are
defined in the [JSON-RPC 2.0 specification](https://www.jsonrpc.org/specification#response_object):

| Field   | Type             | Description                                                        |
|---------|------------------|--------------------------------------------------------------------|
| jsonrpc | String           | **Required.** Must be `"2.0"`.                                     |
| result  | JSON value (any) | **Optional.** Output of a successful method call.                  |
| error   | JSON object      | **Optional.** Error object if the method call failed.              |
| id      | Unsigned integer | **Required.** Unique identifier of the request being responded to. |

The `result` and the `error` fields are mutually exclusive, meaning that either one of them must be present, but not
both. For a successful method call, the `result` field must always be present, and the `error` field must be omitted.
For a failed method call, on the other hand, the `error` field must always be present, and the `result` field must be
omitted. The `error` field is a JSON object according to the
[JSON-RPC 2.0 specification](https://www.jsonrpc.org/specification#error_object), which contains a `code` and
a `message` describing the error. The `id` field must always be present and must match the `id` of the request being
responded to.

Examples:

- Successful response message:
  ```json
  {
    "jsonrpc": "2.0",
    "result": "pong",
    "id": 1000
  }
  ```
- Response with an error:
  ```json
  {
    "jsonrpc": "2.0",
    "error": {
      "code": -32601,
      "message": "method not found"
    },
    "id": 2000
  }
  ```

### Available Icinga Notifications Methods

Currently, Icinga Notifications provides the following methods for channel plugins to call:

| Method       | Description                                             |
|--------------|---------------------------------------------------------|
| channel::Log | Logs a message to Icinga Notifications' logging system. |

#### Log

The `channel::Log` method allows channel plugins to log messages to Icinga Notifications' logging system.
The request's `params` field must be a JSON object with the following fields:

| Field   | Type   | Description                                                                  |
|---------|--------|------------------------------------------------------------------------------|
| level   | String | **Required.** Log level, one of `"debug"`, `"info"`, `"warn"`, or `"error"`. |
| message | String | **Required.** Log message to be logged.                                      |
| fields  | Array  | **Optional.** Additional fields to be logged as key-value pairs.             |

If `fields` are defined, they must be an array of an even number of elements, with each pair representing a key-value
pair. Also, the request's `id` field must be omitted, as the request is expected to be a
[JSON-RPC Notification](https://www.jsonrpc.org/specification#notification), meaning that no response is expected from
Icinga Notifications.

- Example Log Request:
  ```json
  {
    "jsonrpc": "2.0",
    "method": "channel::Log",
    "params": {
      "level": "debug",
      "message": "Sending notification to Slack channel #general",
      "fields": [
        "notification_id",
        12345
      ]
    }
  }
  ```

This request will result in a log message in Icinga Notifications' logging system with the following format:

```
... DEBUG	channel	Sending notification to Slack channel #general	{"channel": {"id": 2, "name": "slack", "type": "slack"}, "pid": 2382, "notification_id": 12345}
```

Icinga Notifications will also log the channel's `id` and `name`, as well as the process ID of the channel plugin
process, which can be useful for debugging purposes.

### RPC Methods

The following methods must be implemented by a channel.

#### GetInfo

The parameterless `GetInfo` method returns information about the channel.

Its `result` is expected to be a JSON object with the `json` fields defined in the
[`Info` type](https://pkg.go.dev/github.com/icinga/icinga-go-library/notifications/plugin#Info).
The `config_attrs` field must be an array of JSON objects according to the
[`ConfigOption` type](https://pkg.go.dev/github.com/icinga/icinga-go-library/notifications/plugin#ConfigOption).
These attributes define configuration options for the channel to be set by the `SetConfig` method.
They are also used for channel configuration in Icinga Notifications Web.

##### Example GetInfo Request

```json
{
  "jsonrpc": "2.0",
  "method": "GetInfo",
  "id": 1
}
```

##### Example GetInfo Response

```json
{
  "jsonrpc": "2.0",
  "result": {
    "name": "Minified Webhook",
    "version": "0.0.0-gf369a11-dirty",
    "author": "Icinga GmbH",
    "config_attrs": [
      {
        "name": "url_template",
        "type": "string",
        "label": {
          "de_DE": "URL-Template",
          "en_US": "URL Template"
        },
        "help": {
          "de_DE": "URL, optional als Go-Template über das zu verarbeitende plugin.NotificationRequest.",
          "en_US": "URL, optionally as a Go template over the current plugin.NotificationRequest."
        },
        "required": true,
        "min": null,
        "max": null
      },
      {
        "name": "response_status_codes",
        "type": "string",
        "label": {
          "de_DE": "Antwort-Status-Codes",
          "en_US": "Response Status Codes"
        },
        "help": {
          "de_DE": "Kommaseparierte Liste erwarteter Status-Code der HTTP-Antwort, z.B.: 200,201,202,208,418",
          "en_US": "Comma separated list of expected HTTP response status code, e.g., 200,201,202,208,418"
        },
        "default": "200",
        "min": null,
        "max": null
      }
    ]
  },
  "id": 1
}
```

#### SetConfig

The `SetConfig` method configures the channel.

Icinga Notifications will call this method at least once on each channel before sending the first notification to
initialize the channel plugin. However, it may also be called multiple times during the lifetime of a channel plugin,
for example, if the user changes the configuration in Icinga Notifications Web. Therefore, the channel plugin must be
able to handle multiple calls to `SetConfig` and update its configuration accordingly at any given time.

The passed JSON object in the request's `params` field reflects the objects from `GetInfo`'s `config_attrs`.
Each object in the `config_attrs` array must be configurable, using its `name` attribute as a key together along with
the desired configuration value, which must be of the type specified in the `type` field.

To illustrate, the URL template from the above output is configurable with the following JSON object passed in `params`:

```json
{
  "url_template": "http://localhost:8000/update/{{.Incident.Id}}"
}
```

After the channel plugin has successfully applied the configuration, it must return a valid [JSON-RPC response](#response)
with the `result` field set to a simple success message or just to JSON `null`. If the channel plugin is unable to
apply the configuration, it must return a valid [JSON-RPC response](#response) with the `error` field set to the
appropriate error code and message.

##### Example SetConfig Request

```json
{
  "jsonrpc": "2.0",
  "method": "SetConfig",
  "params": {
    "url_template": "http://localhost:8000/update/{{.Incident.Id}}",
    "response_status_codes": "200"
  },
  "id": 2
}
```

##### Example SetConfig Response

```json
{
  "jsonrpc": "2.0",
  "result": null,
  "id": 2
}
```

#### SendNotification

The `SendNotification` method requests the channel to dispatch notifications.

Within the request's `params`, a JSON object representing a
[`NotificationRequest`](https://pkg.go.dev/github.com/icinga/icinga-go-library/notifications/plugin#NotificationRequest)
is passed.

If the channel is unable to send a notification, an `error` response must be replied with the appropriate error code
and message. This may be due to channel-specific reasons, such as an email channel where the SMTP server is unavailable,
or if the channel is missing required configuration values.

##### Example SendNotification Request

```json
{
  "jsonrpc": "2.0",
  "method": "SendNotification",
  "params": {
    "contact": {
      "full_name": "icingaadmin",
      "addresses": [
        {
          "type": "email",
          "address": "icingaaadmin@example.com"
        }
      ]
    },
    "object": {
      "name": "dummy-816!random fortune",
      "url": "http://localhost/icingaweb2/icingadb/service?name=random%20fortune&host.name=dummy-816",
      "tags": {
        "host": "dummy-816",
        "service": "random fortune"
      }
    },
    "incident": {
      "id": 1437,
      "url": "http://localhost/icingaweb2/notifications/incident?id=1437",
      "severity": "crit"
    },
    "event": {
      "time": "2024-07-12T10:47:30.445439055Z",
      "message": "Q:\tWhat looks like a cat, flies like a bat, brays like a donkey, and\n\tplays like a monkey?\nA:\tNothing."
    }
  },
  "id": 3
}
```

##### Example SendNotification Response

```json
{
  "jsonrpc": "2.0",
  "result": null,
  "id": 3
}
```

### Channel Configuration

A channel offers its configuration options through its response to the [`GetInfo` method call](#getinfo).
Each configuration option is described by a
[`ConfigOption` entry](https://pkg.go.dev/github.com/icinga/icinga-go-library/notifications/plugin#ConfigOption)
in the `config_attrs` array.
This information will be queried once by Icinga Notifications upon startup and stored in the database.

Depending on the `ConfigOption`'s type, Icinga Notifications Web will render different form element.
For example, a `text` will result in an input box, while `options` will result in a multi-select list.

When a user configures a Channel in Icinga Notifications Web, the configuration will be stored in the database as JSON.
More specifically, a JSON object mapping `config_attrs.name` to the configured value is stored,
as expected as the `params` for the [`SetConfig` method](#setconfig).

Since `ConfigOption`s may have defaults defined,
Icinga Notifications Web will not add unchanged defaults to the configuration JSON object.
Therefore, the channel plugin is expected to use their offered default value if the key-value pair is absent.

A `SetConfig` implementation may follow this logic by first setting its defaults and
then overwriting its state based on the configuration received via the `params`.

Finally, Icinga Notifications will start a new process for this channel and
pass the stored JSON object to the channel by calling the `SetConfig` method.
The process is kept alive and receives occasional [`SendNotification` method calls](#sendnotification) until
Icinga Notifications is stopped or the channel plugin is restarted due to a configuration change. Note that Icinga
Notifications will not restart the process on every configuration change, unless it has no other choice. For instance,
if the channel plugin config changes at runtime, Icinga Notifications will call `SetConfig` again with the new config
except when the channel plugin type has changed to a different one, in which case the old channel plugin process will
be stopped and a new one will be started.

## Writing Channel Plugins

!!! tip

    Icinga Notifications comes with a Webhook channel plugin.
    Consider using this channel if your transport uses HTTP instead of writing a custom channel.

!!! tip

    When developing custom channels, consider naming them with a unique prefix,
    as additional channels will be added to Icinga Notifications in the future.
    For example, name your channel `x_irc` or `my_irc` instead of `irc`.

The channels that ship with Icinga Notifications can only cover some use cases.
Therefore, we encourage you to develop your own channels that cover your specific needs.

### Writing Channel Plugins in Go

Since Icinga Notifications and all of its channels are written in the Go programming language,
libraries already used internally can be reused. In particular, the
[Icinga Notifications Plugin](https://pkg.go.dev/github.com/icinga/icinga-go-library/notifications/plugin) and
[Icinga Notifications JSON-RPC](https://pkg.go.dev/github.com/icinga/icinga-go-library/notifications/jsonrpc) packages
provide a framework for writing channel plugins in Go. The `jsonrpc` package implements the JSON-RPC 2.0 protocol,
while the `plugin` package provides types and functions for implementing channel plugins. Also, the `plugin` package
provides a [`Run`](https://pkg.go.dev/github.com/icinga/icinga-go-library/notifications/plugin#Run) function that takes
care of setting up a JSON-RPC server and dispatching requests to the appropriate methods of the channel plugin.

To respect the [channel configuration logic](#channel-configuration) described above,
an implementation of the `SetConfig` method should start by calling
[`PopulateDefaults`](https://pkg.go.dev/github.com/icinga/icinga-go-library/notifications/plugin#PopulateDefaults).
Also, note that Icinga Notifications might call `SetConfig` multiple times during the lifetime of a channel plugin,
so the implementation should be able to handle multiple calls and update its configuration accordingly.
This is further explained in the documentation of the `SetConfig` method.

Also, the jsonrpc implementation in the SDK is designed to allow for bidirectional communication, meaning that the
channel plugin can also send requests to Icinga Notifications. Please refer to the
[Available Icinga Notifications Methods](#available-icinga-notifications-methods) section for more information on the
methods available to channel plugins.

To debug the JSON-RPC communication, build the channel plugin with the `DebugJsonRpc` build tag, which will log all
requests and responses to `stderr`.

For concrete examples, there are the implemented channels in the Icinga Notifications repository at
[`./cmd/channels`](https://github.com/Icinga/icinga-notifications/tree/main/cmd/channels).