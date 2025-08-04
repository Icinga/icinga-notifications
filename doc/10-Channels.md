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

!!! warning

    As this is still an early preview version, things might change.
    There may still be severe bugs and incompatible changes may happen without any notice.

Channel plugins are independent processes that run continuously, started and supervised by Icinga Notifications.
They receive JSON-formatted requests on `stdin` and reply with JSON-formatted responses on `stdout`.
For logging or debugging purposes, channels can write to `stderr`,
which is being forwarded to the Icinga Notifications log.

### RPC Architecture

The request and response structure is inspired by JSON-RPC.
Both the general anatomy of requests and responses as well as the specific methods are described below.
Note that fields marked as optional must be omitted from the JSON object if they do not have a value.

This documentation uses beautified JSON for ease of reading.

#### Request

A channel receives a request as a JSON object with the following fields:

| Field    | Type             | Description                                  |
|----------|------------------|----------------------------------------------|
| `method` | String           | **Required.** Request method to call.        |
| `params` | JSON object      | **Optional.** Params for the request method. |
| `id`     | Unsigned integer | **Required.** Unique identifier.             |

The `params` field is optional because some methods do not require parameters.
If they are required for a particular method,
they are specified along with the expected value type in the method description below.

Each request contains a unique `id` that must be echoed back in the channel's response.
This allows Icinga Notifications to associate a response with its request.

Examples:

- Simple request without any `params`:
  ```json
  {
    "method": "Simple",
    "id": 1000
  }
  ```
- Request with `params` of different types:
  ```json
  {
    "method": "WithParams",
    "params": {
      "foo": 23,
      "bar": "hello"
    },
    "id": 1000
  }
  ```

#### Response

Each request must be answered by the channel with a JSON object of the following fields:

| Field    | Type             | Description                                       |
|----------|------------------|---------------------------------------------------|
| `result` | JSON value (any) | **Optional.** Output of a successful method call. |
| `error`  | String           | **Optional.** Error message.                      |
| `id`     | Unsigned integer | **Required.** Request `id`.                       |

The `result` and the `error` fields are mutually exclusive.
A `result` can be omitted when the method does not return a value, i.e., for setter calls.
However, in case of a present `error` value, the `result` field must be omitted.
Thus, a successful response without a `result` contains only an `id` field.

Examples:

- Successful response without a `result` message:
  ```json
  {
    "id": 1000
  }
  ```
- Successful response with a `result`:
  ```json
  {
    "result": "hello world",
    "id": 1000
  }
  ```
- Response with an error:
  ```json
  {
    "error": "unknown method: 'Foo'",
    "id": 1000
  }
  ```

### RPC Methods

The following methods must be implemented by a channel.

#### GetInfo

The parameterless `GetInfo` method returns information about the channel.

Its `result` is expected to be a JSON object with the `json` fields defined in the
[`Info` type](https://pkg.go.dev/github.com/icinga/icinga-notifications/pkg/plugin#Info).
The `config_attrs` field must be an array of JSON objects according to the
[`ConfigOption` type](https://pkg.go.dev/github.com/icinga/icinga-notifications/pkg/plugin#ConfigOption).
These attributes define configuration options for the channel to be set by the `SetConfig` method.
They are also used for channel configuration in Icinga Notifications Web.

##### Example GetInfo Request

```json
{
  "method": "GetInfo",
  "id": 1
}
```

##### Example GetInfo Response

```json
{
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

The Icinga Notifications daemon will call this method at least once on each channel
before sending the first notifications to initialize the channel plugin.

The passed JSON object in the request's `param` field reflects the objects from `GetInfo`'s `config_attrs`.
Each object in the `config_attrs` array must be configurable,
using its `name` attribute as a key together along with the desired configuration value,
which must be of the type specified in the `type` field.

To illustrate, the URL template from the above output is configurable with the following JSON object passed in `params`:

```json
{
  "url_template": "http://localhost:8000/update/{{.Incident.Id}}"
}
```

If the channel plugin has successfully configured itself, a response without a `result` must be returned.
Otherwise, if the channel decides that the provided configuration is incorrect, an `error` must be returned.
This may happen if, for example, an invalid configuration value was given.

##### Example SetConfig Request

```json
{
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
  "id": 2
}
```

#### SendNotification

The `SendNotification` method requests the channel to dispatch notifications.

Within the request's `params`, a JSON object representing a
[`NotificationRequest`](https://pkg.go.dev/github.com/icinga/icinga-notifications/pkg/plugin#NotificationRequest)
is passed.

If the channel is unable to send a notification, an `error` must be returned.
This may be due to channel-specific reasons, such as an email channel where the SMTP server is unavailable,
or if the channel is missing required configuration values.

##### Example SendNotification Request

```json
{
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
      },
    },
    "incident": {
      "id": 1437,
      "url": "http://localhost/icingaweb2/notifications/incident?id=1437",
      "severity": "crit"
    },
    "event": {
      "time": "2024-07-12T10:47:30.445439055Z",
      "type": "state",
      "username": "",
      "message": "Q:\tWhat looks like a cat, flies like a bat, brays like a donkey, and\n\tplays like a monkey?\nA:\tNothing."
    }
  },
  "id": 3
}
```

##### Example SendNotification Response

```json
{
  "id": 3
}
```

### Channel Configuration

A channel offers its configuration options through its response to the [`GetInfo` method call](#getinfo).
Each configuration option is described by a
[`ConfigOption` entry](https://pkg.go.dev/github.com/icinga/icinga-notifications/pkg/plugin#ConfigOption)
in the `config_attrs` array.
Those information will be queried once by Icinga Notifications upon startup and stored in the database.

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
The process is kept alive and receives occasional [`SendNotification` method calls](#sendnotification).

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

!!! warning

    As this is still an early preview version, things might change.
    There may still be severe bugs and incompatible changes may happen without any notice.

Since Icinga Notifications and all of its channels are written in the Go programming language,
libraries already used internally can be reused.
In particular, the [`Plugin`](https://pkg.go.dev/github.com/icinga/icinga-notifications/pkg/plugin#Plugin)
interface must be implemented, requesting methods for all the RPC methods described above.

To respect the [channel configuration logic](#channel-configuration) described above,
an implementation of `SetConfig` should start by calling
[`PopulateDefaults`](https://pkg.go.dev/github.com/icinga/icinga-notifications/pkg/plugin#PopulateDefaults).

The channel plugin's `main` function should call
the [`RunPlugin`](https://pkg.go.dev/github.com/icinga/icinga-notifications/pkg/plugin#RunPlugin) function,
taking care about calling the RPC method implementations.

For concrete examples, there are the implemented channels in the Icinga Notifications repository at
[`./cmd/channels`](https://github.com/Icinga/icinga-notifications/tree/main/cmd/channels).