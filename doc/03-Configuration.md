# Configuration

The configuration for Icinga Notifications is twofold.
The main configuration resides in the database,
shared between the Icinga Notifications daemon and Icinga Notifications Web.
However, as the Icinga Notifications daemon needs to know how to access this database and some further settings,
it needs its own configuration file as well.

This configuration is stored in `/etc/icinga-notifications/config.yml`.
See [config.example.yml](../config.example.yml) for an example configuration.

## Top Level Configuration

### HTTP API Configuration

The HTTP API listener can be used both for submission and for debugging purposes.

| Option         | Description                                                          |
|----------------|----------------------------------------------------------------------|
| listen         | Address to bind to, port included. (Example: `localhost:5680`)       |
| debug-password | Password expected via HTTP Basic Authentication for debug endpoints. |

### Icinga Web 2

The `icingaweb2-url` is expected to point to the base directory of your Icinga Web 2 installation,
i.e., `https://example.com/icingaweb2/`, to be used for URL creation.

### Channels Directory

All available Icinga Notifications channels should reside in the `channels-dir` directory.
For a package installation, the default will point to the correct location and must not be changed.

This directory should be `/usr/libexec/icinga-notifications/channels` on systems that follow the Filesystem Hierarchy Standard.
It may also be `/usr/lib/icinga-notifications/channels`, depending on the operating system conventions.

### API Timeout

The `api-timeout` specifies the Icinga 2 API request timeout defined as a [duration string](#duration-string).
Note, this timeout does not apply to the Icinga 2 event streams, but to those API endpoints
like `/v1/objects`, `/v1/status` used to occasionally retrieve some additional information of a Checkable.

## Database Configuration

Connection configuration for the database where Icinga Notifications stores configuration and historical data.
This is also the database used in Icinga Notifications Web to view and work with the data.

| Option   | Description                                                                                                                                               |
|----------|-----------------------------------------------------------------------------------------------------------------------------------------------------------|
| type     | **Optional.** Either `mysql` (default) or `pgsql`.                                                                                                        |
| host     | **Required.** Database host or absolute Unix socket path.                                                                                                 |
| port     | **Optional.** Database port. By default, the MySQL or PostgreSQL port, depending on the database type.                                                    |
| database | **Required.** Database name.                                                                                                                              |
| user     | **Required.** Database username.                                                                                                                          |
| password | **Optional.** Database password.                                                                                                                          |
| tls      | **Optional.** Whether to use TLS.                                                                                                                         |
| cert     | **Optional.** Path to TLS client certificate.                                                                                                             |
| key      | **Optional.** Path to TLS private key.                                                                                                                    |
| ca       | **Optional.** Path to TLS CA certificate.                                                                                                                 |
| insecure | **Optional.** Whether not to verify the peer.                                                                                                             |
| options  | **Optional.** List of low-level [database options](#database-options) that can be set to influence some Icinga Notifications internal default behaviours. |

### Database Options

Each of these configuration options are highly technical with thoroughly considered and tested default values that you
should only change when you exactly know what you are doing. You can use these options to influence the Icinga Notifications default
behaviour, how it interacts with databases, thus the defaults are usually sufficient for most users and do not need any
manual adjustments.

!!! important

    Do not change the defaults if you do not have to!

| Option                         | Description                                                                                                                                                 |
|--------------------------------|-------------------------------------------------------------------------------------------------------------------------------------------------------------|
| max_connections                | **Optional.** Maximum number of database connections Icinga Notifications is allowed to open in parallel if necessary. Defaults to `16`.                    |
| max_connections_per_table      | **Optional.** Maximum number of queries Icinga Notifications is allowed to execute on a single table concurrently. Defaults to `8`.                         |
| max_placeholders_per_statement | **Optional.** Maximum number of placeholders Icinga Notifications is allowed to use for a single SQL statement. Defaults to `8192`.                         |
| max_rows_per_transaction       | **Optional.** Maximum number of rows Icinga Notifications is allowed to `SELECT`,`DELETE`,`UPDATE` or `INSERT` in a single transaction. Defaults to `8192`. |
| wsrep_sync_wait                | **Optional.** Enforce [Galera cluster](#galera-cluster) nodes to perform strict cluster-wide causality checks. Defaults to `7`.                             |

## Logging Configuration

Configuration of the logging component used by Icinga Notifications.

| Option   | Description                                                                                                                                                                                                                                           |
|----------|-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| level    | **Optional.** Specifies the default logging level. Can be set to `fatal`, `error`, `warn`, `info` or `debug`. Defaults to `info`.                                                                                                                     |
| output   | **Optional.** Configures the logging output. Can be set to `console` (stderr) or `systemd-journald`. Defaults to systemd-journald when running under systemd, otherwise to console. See notes below for [systemd-journald](#systemd-journald-fields). |
| interval | **Optional.** Interval for periodic logging defined as [duration string](#duration-string). Defaults to `"20s"`.                                                                                                                                      |
| options  | **Optional.** Map of component name to logging level in order to set a different logging level for each component instead of the default one. See [logging components](#logging-components) for details.                                              |

### Logging Components

| Component       | Description                                                               |
|-----------------|---------------------------------------------------------------------------|
| channel         | Notification channels, their configuration and output.                    |
| database        | Database connection status and queries.                                   |
| icinga2         | Icinga 2 API communications, including the Event Stream.                  |
| incident        | Incident management and changes.                                          |
| listener        | HTTP listener for event submission and debugging.                         |
| runtime-updates | Configuration changes through Icinga Notifications Web from the database. |

## Appendix

### Duration String

A duration string is a sequence of decimal numbers and a unit suffix, such as `"20s"`.
Valid units are `"ms"`, `"s"`, `"m"` and `"h"`.

### Galera Cluster

Icinga Notifications expects a more consistent behaviour from its database than a
[Galera cluster](https://mariadb.com/kb/en/what-is-mariadb-galera-cluster/) provides by default. To accommodate this,
Icinga Notifications sets the [wsrep_sync_wait](https://mariadb.com/kb/en/galera-cluster-system-variables/#wsrep_sync_wait) system
variable for all its database connections. Consequently, strict cluster-wide causality checks are enforced before
executing specific SQL queries, which are determined by the value set in the `wsrep_sync_wait` system variable.
By default, Icinga Notifications sets this to `7`, which includes `READ, UPDATE, DELETE, INSERT, REPLACE` query types and is
usually sufficient. Unfortunately, this also has the downside that every single Icinga Notifications query will be blocked until
the cluster nodes resynchronise their states after each executed query, and may result in degraded performance.

However, this does not necessarily have to be the case if, for instance, Icinga Notifications is only allowed to connect to a
single cluster node at a time. This is the case when a load balancer does not randomly route connections to all the
nodes evenly, but always to the same node until it fails, or if your database cluster nodes have a virtual IP address
fail over assigned. In such situations, you can set the `wsrep_sync_wait` system variable to `0` in the
`/etc/icinga-notifications/config.yml` file to disable it entirely, as Icinga Notifications doesn't have to wait for cluster
synchronisation then.

### Systemd Journald Fields

When examining the journal with `journalctl`, fields containing additional information are hidden by default.
Setting an appropriate
[`--output` option](https://www.freedesktop.org/software/systemd/man/latest/journalctl.html#Output%20Options)
will include them, such as: `--output verbose` or `--output json`.
For example:

```
journalctl --unit icinga-notifications.service --output verbose
```

All Icinga Notifications fields are prefixed with `ICINGA_NOTIFICATIONS_`,
e.g., `ICINGA_NOTIFICATIONS_ERROR` for error messages.
