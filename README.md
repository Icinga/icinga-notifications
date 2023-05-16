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
It is required that you have created a new database and imported the [schema](schema/pgsql/schema.sql) file beforehand.
> **Note**
> At the moment **PostgreSQL** is the only database backend we support.

Additionally, it also requires you to manually insert items into the **source** table before starting the daemon.
```sql
INSERT INTO source (id, type, name) VALUES (1, 'icinga2', 'Icinga 2')
```

Then, you can launch the daemon with the following command.
```go
go run ./cmd/icinga-notifications-daemon --config config.yml
```

Last but not least, in order for the daemon to receive events from Icinga 2, you need to copy the [Icinga 2 config](icinga2.conf)
to `/etc/icinga2/features-enabled` on your master node(s) and restart the Icinga 2 service. At the top of this file,
you will find multiple configurations options that can be set in `/etc/icinga2/constants.conf`. There are also Icinga2
`EventCommand` definitions in this file that will automatically match all your **checkables**, which may not work
properly if the configuration already uses event commands for something else.

## License

Icinga Notifications is licensed under the terms of the [GNU General Public License Version 2](LICENSE).
