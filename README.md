# Icinga Notifications

> **Warning**
> This is an early preview version for you to try, but do not use this in production. There may still be severe bugs
> and incompatible changes may happen without any notice. At the moment, we don't yet provide any support for this.

Icinga Notifications is a set of components that processes received events from various sources, manages incidents and
forwards notifications to predefined contacts, consisting of:

* The Icinga Notifications daemon (this repository), which receives events and sends notifications
* An [Icinga Web module](https://github.com/Icinga/icinga-notifications-web) that provides graphical configuration and further processing of the data collected by the daemon
* And Icinga 2 and other custom sources that propagate state updates and acknowledgement events to the daemon

## Installation

To install Icinga Notifications and get started, you first need to clone this repository.
```bash
git clone https://github.com/Icinga/icinga-notifications.git
```

Next, you need to provide a `config.yml` file, similar to the [example config](config.example.yml), for the daemon.
It is also possible to set environment variables by name instead of or in addition to the configuration file.
The environment variable key is an underscore separated string of uppercase struct fields. For example
* `ICINGA_NOTIFICATIONS_LISTEN` sets `ConfigFile.Listen` and
* `ICINGA_NOTIFICATIONS_DATABASE_HOST` sets `ConfigFile.Database.Host`.

It is required that you have created a new database and imported the [schema](schema/pgsql/schema.sql) file beforehand.
> **Note**
> At the moment **PostgreSQL** is the only database backend we support.

Additionally, it also requires you to manually insert items into the **source** table before starting the daemon.
```sql
INSERT INTO source
    (id, type, name, icinga2_base_url, icinga2_auth_user, icinga2_auth_pass, icinga2_insecure_tls)
VALUES
    (1, 'icinga2', 'Local Icinga 2', 'https://localhost:5665', 'root', 'icinga', 'y');
```

Then, you can launch the daemon with the following command.
```go
go run ./cmd/icinga-notifications-daemon --config config.yml
```

## License

Icinga Notifications is licensed under the terms of the [GNU General Public License Version 2](LICENSE).
