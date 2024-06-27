<!-- {% if index %} -->
# Installing Icinga Notifications

The recommended way to install Icinga Notifications is to use prebuilt packages
for all supported platforms from our official release repository.
Please follow the steps listed for your target operating system,
which guide you through setting up the repository and installing Icinga Notifications.

To upgrade an existing Icinga Notifications installation to a newer version,
see the [Upgrading](04-Upgrading.md) documentation for the necessary steps.

<!-- {% else %} -->

## Setting up the Database

A MySQL (≥5.5), MariaDB (≥10.1), or PostgreSQL (≥9.6) database is required to run Icinga Notifications.
Please follow the steps listed for your target database,
which guide you through setting up the database and user and importing the schema.

### Setting up a MySQL or MariaDB Database

If you use a version of MySQL < 5.7 or MariaDB < 10.2, the following server options must be set:

```
innodb_file_format=barracuda
innodb_file_per_table=1
innodb_large_prefix=1
```

Set up a MySQL database for Icinga Notifications:

```
# mysql -u root -p

CREATE DATABASE notifications;
CREATE USER 'notifications'@'localhost' IDENTIFIED BY 'CHANGEME';
GRANT ALL ON notifications.* TO 'notifications'@'localhost';
```

After creating the database, import the Icinga Notifications schema using the following command:

```
mysql -u root -p notifications < /usr/share/icinga-notifications/schema/mysql/schema.sql
```

### Setting up a PostgreSQL Database

Set up a PostgreSQL database for Icinga Notifications:

```
# su -l postgres

createuser -P notifications
createdb -E UTF8 --locale en_US.UTF-8 -T template0 -O notifications notifications
echo 'CREATE EXTENSION IF NOT EXISTS citext;' | psql notifications
```

The `CREATE EXTENSION` command requires the `postgresql-contrib` package.

Edit `pg_hba.conf`, insert the following before everything else:

```
local all notifications           md5
host  all notifications 0.0.0.0/0 md5
host  all notifications      ::/0 md5
```

To apply these changes, run `systemctl reload postgresql`.

After creating the database, import the Icinga Notifications schema using the following command:

```
psql -U icinga-notifications notifications < /usr/share/icinga-notifications/schema/pgsql/schema.sql
```

## Configuring Icinga Notifications

Icinga Notifications installs its configuration file to `/etc/icinga-notifications/config.yml`,
pre-populating most of the settings for a local setup. Before running Icinga Notifications,
adjust the database credentials and the Icinga Web 2 URL.
The configuration file explains general settings.
All available settings can be found under [Configuration](03-Configuration.md).

## Configuring Icinga 2

Icinga Notifications utilizes the [Icinga 2 API](https://icinga.com/docs/icinga-2/latest/doc/12-icinga2-api/)
to fetch events from an Icinga 2 instance.
Thus, if the Icinga 2 API is not already configured,
please follow the [API setup guide](https://icinga.com/docs/icinga-2/latest/doc/12-icinga2-api/#setting-up-the-api) now.
Furthermore, an [`ApiUser`](https://icinga.com/docs/icinga-2/latest/doc/09-object-types/#objecttype-apiuser)
with [permissions](https://icinga.com/docs/icinga-2/latest/doc/12-icinga2-api/#icinga2-api-permissions)
as listed in the example below is required.

```
object ApiUser "notifications" {
  password = "insecure"
  permissions = [ "events/*", "objects/query/*" ]
}
```

After completing setting up Icinga Notifications Web, the Icinga 2 API can be configured with this `ApiUser` as a source.

## Running Icinga Notifications

The `icinga-notifications` package automatically installs the necessary systemd unit files to run Icinga Notifications.
Please run the following command to enable and start its service:

```
systemctl enable --now icinga-notifications
```

## Installing Icinga Notifications Web

With Icinga 2, Icinga Notifications and the database fully set up, it is now time to install Icinga Notifications Web,
which connects to the database and allows configuring Icinga Notifications.

Please follow the
[Icinga Notifications Web documentation](https://icinga.com/docs/icinga-notifications-web/latest/doc/02-Installation/).

<!-- {% endif %} --><!-- {# end else if index #} -->
