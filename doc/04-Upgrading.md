# Upgrading Icinga Notifications

Some Icinga Notifications upgrades require manual intervention, others do not. If you need to intervene, the release notes will
point you to the specific upgrade section on this page.

Please note that version upgrades are incremental. If you are upgrading across multiple versions, make sure to follow
the steps for each of them.

## Database Schema Upgrades

Certain Icinga Notifications version upgrades require a database schema upgrade. If the upgrade section of the specific Icinga Notifications
release mentions a schema upgrade, this section will guide you through the process of applying the schema upgrade.

First, stop the Icinga Notifications daemon.

```
systemctl stop icinga-notifications
```

Locate the required schema upgrade files in `/usr/share/icinga-notifications/schema/mysql/upgrades/` for MySQL/MariaDB or in
`/usr/share/icinga-notifications/schema/pgsql/upgrades/` for PostgreSQL. The schema upgrade files are named after the new Icinga Notifications
release and are mentioned in the specific section below. If you have skipped multiple Icinga Notifications releases, apply all
schema versions in their order, starting with the earliest release.

The following commands would apply a sample version 0.1.2 schema upgrade to the `notifications` database as the `notifications`
user. Please modify them for your setup and the schema upgrade you want to apply.

!!! important

    For PostgreSQL, the schema upgrade must be applied by the `notifications` PostgreSQL user, since this user owns the
    current tables and would own any new table created by the schema upgrade.
    If you are unsure whether your PostgreSQL user is named `notifications`, as stated in the installation section,
    you can list the _Owner_ for each table in the `notifications` database via `\d` in `psql`.

    ```
    $ psql -U postgres notifications -c '\d'
                            List of relations
     Schema |               Name               |   Type   |    Owner
    --------+----------------------------------+----------+--------------
     public | available_channel_type           | table    | notifications
     public | browser_session                  | table    | notifications
     public | channel                          | table    | notifications
    [ . . . ]
    ```

    This shortened output shows that `notfications` is the _Owner_ and needs to be set as `-U notifications` in the following
    upgrade command.

* MySQL/MariaDB:
  ```
  mysql -u notifications -p notifications < /usr/share/icinga-notifications/schema/mysql/upgrades/0.1.2.sql
  ```
* PostgreSQL:
  ```
  psql -U notifications notifications < /usr/share/icinga-notifications/schema/pgsql/upgrades/0.1.2.sql
  ```

Afterwards, restart Icinga Notifications.

```
systemctl start icinga-notifications
```

## Upgrading to Icinga Notifications v0.2.0

This Icinga Notifications release moves the Icinga event source from Icinga 2 to Icinga DB.
For this change, Icinga DB is now a required component and some manual changes are necessary, as described below.

### Requirements

Version 0.2.0 of Icinga Notifications is released alongside
- Icinga Notifications Web 0.2.0,
- Icinga DB 1.5.0, and
- Icinga DB Web 1.3.0.

Ensure that the new requirements are met before updating Icinga Notifications.

### Schema

The upgrade script `0.2.0.sql` must be applied as described in the [schema upgrade section](#database-schema-upgrades).

### Configuration Changes

Please apply the following changes in this order to migrate from an Icinga 2 source to an Icinga DB source.

Start with removing the `api-timeout` option from Icinga Notifications configuration file `/etc/icinga-notifications/config.yml`.
This entry was used to access the Icinga 2 API and became obsolete.

If you have created an `ApiUser` solely for Icinga Notifications in the Icinga 2 configuration somewhere under `/etc/icinga2/`, delete it.

After applying these changes, please restart Icinga 2 and Icinga Notifications.

```
systemctl restart icinga2 icinga-notifications
```

Now log into Icinga Web 2, navigate to the module configuration, and open `notifications`.
Select the _Sources_ tab and update the username and password for each former Icinga 2 source, being an Icinga DB source now.

Update the Icinga DB configuration file `/etc/icingadb/config.yml` for each new Icinga DB source and add the new
[`notifications` configuration option](https://icinga.com/docs/icinga-db/latest/doc/03-Configuration/#notifications-configuration).
 - The `url` points to your Icinga Notifications API, allowing Icinga DB to submit events.
   When running Icinga Notifications on the same host as Icinga DB, this is typically `http://localhost:5680/`.
 - The `username` and `password` are used to authenticate the Icinga DB source.
   These are the login credentials just configured in Icinga Notifications Web.

Now, restart Icinga DB.

```
systemctl restart icingadb
```

Back in Icinga Web 2, click on _Notifications_ in the left sidebar, select _Configuration_, and open the _Event Rules_ tab.
With this update, rules are now bound to sources.
Thus, check for each rule that they apply to the desired source by selecting the rule and editing it via the edit icon right to its name.
In case some rules of yours should apply to multiple sources, please duplicate these rules.

The migration should be finished now.
Please take a look at the other configuration options and verify that Icinga Notifications works as intended.
Thanks for following this migration.
