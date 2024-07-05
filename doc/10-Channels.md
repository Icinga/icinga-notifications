# Channels

After Icinga Notifications decides to send a notification of any kind, it will be passed to a channel plugin.
Such a channel plugin submits the notification event to a channel, e.g., email or a chat client.

Icinga Notifications comes packed with channel plugins, but also enables you to develop your own plugins.

To make those plugins available to Icinga Notifications, they must be placed in the
[channels directory](03-Configuration.md#channels-directory),
which is being done automatically for package installations.
Afterwards they can be configured through Icinga Notifications Web.

## Technical Channel Description

Channel plugins are processes that run continuously and independently of each other. They receive many requests over
their lifetime. They receive JSON-formatted requests on stdin and reply with JSON-formatted responses on stdout. The
request and response structure is inspired by JSON-RPC.

### Request

The request must be in JSON format and should contain following keys:

- `method`: The request method to call.
- `params`: The params for request method.
- `id`: Unsigned int value. Required to assign the response to its request as responses can be sent out of order.

Examples:

```json
{
  "method": "Add",
  "params": {
    "num1": 5,
    "num2": 3
  },
  "id": 2020
}
```

```json
{
  "method": "Foo",
  "params": {
    "a": "value1",
    "b": "value2"
  },
  "id": 3030
}
```

### Response

The response is in JSON format and contains following keys:

- `result`: The result as JSON format. Omitted when the method does not return a value (e.g. setter calls) or an error
  has occurred.
- `error`: The error message. Omitted when no error has occurred.
- `id`: The request id. When result value is empty and no error is occurred, the response will only contain the request
  id.

Examples:

```json
{
  "result": 8,
  "id": 2020
}
```

```json
{
  "error": "unknown method: 'Foo'",
  "id": 3030
}
```

### Methods

Currently, the channel plugin include following three methods:

- `SetConfig`: Initialize the channel plugin with specified config as `params` key. The config is plugin specific
  therefore each plugin defines what is expected as config.
  [(example)](../internal/channel/examples/set-config.json)
- `GetInfo`: Get the information about the channel e.g. Name. The `params` key has no effect and can be omitted.
  [(example)](../internal/channel/examples/get-info.json)
- `SendNotification`: Send the notifications. The `params` key should contain the information about the contact to be
  notified, corresponding object, the incident and the triggered event.
  [(example)](../internal/channel/examples/send-notification.json)
