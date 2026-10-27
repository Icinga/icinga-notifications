# Icinga Notifications Changelog

## 1.0.0 (2026-10-29)

This is the first stable Icinga Notifications release, published alongside [Icinga Notifications Web 1.0.0](https://icinga.com/docs/icinga-notifications-web/latest/).
Since the 0.2.0 preview, large parts of Icinga Notifications were reworked to be ready for production use.

Event rules are evaluated by Icinga Notifications again, based on the object details submitted with each event, such as custom variables.
This enables generic sources again, allowing any tool to submit events via the HTTP API.
Next to escalation rules for incidents, the new notification rules allow notifying contacts without an incident,
e.g., about downtimes, flapping, or acknowledgements.
Multiple Icinga Notifications instances can now share one database for high availability,
and old incidents and notifications can be cleaned up automatically.

Notable changes include the following.

* Evaluate event rules within Icinga Notifications again, allowing generic sources. #418
* Add notification rules to notify contacts about events without opening an incident. #529
* Let subscribers and managers of an incident choose which kinds of events they are notified about. #529, #590
* Support high availability by running multiple Icinga Notifications instances with one database. #443, #463, #487
* Remove closed incidents and old notifications after a configurable retention period. #446, #460
* Support escalation conditions based on whether an incident is managed. #511
* Allow secure connections from sources via HTTPS, TLS client certificates, or Unix domain sockets. #430, #454, #441
* Allow configuration via environment variables and reading passwords from files. #371
* Rework container images, including automatically schema import. #427, #584

### Channels

The channels used to send notifications were extended, and a new Telegram channel was added.
The Rocket.Chat channel was removed.

* Add a Telegram channel. #498
* Drop the Rocket.Chat channel, which can be replaced by the webhook channel. #583
* Support further SMTP authentication methods and TLS options for the email channel. #507, #532
* Support TLS options and custom HTTP headers for the webhook channel. #269
* Improve the default notification messages and include a short summary of the event. #556, #580

### Requirements

Icinga Notifications 1.0.0 is released alongside Icinga Notifications Web 1.0.0, Icinga DB 1.6.0, Icinga DB Web 1.5.0, and Icinga 2 2.16.6.
Using Icinga DB as a source requires Icinga DB 1.6.0 or later.

### Schema

The `1.0.0.sql` schema upgrade is available and must be applied.
Please follow the [schema upgrade documentation](doc/04-Upgrading.md#database-schema-upgrades).

### Configuration Changes

The configuration file was restructured and the `icingaweb2-url` option was moved to Icinga DB.
Furthermore, object filters of existing event rules must be recreated in Icinga Notifications Web.
Please read the [upgrading documentation](doc/04-Upgrading.md#upgrading-to-icinga-notifications-v100) carefully.

## 0.2.0 (2025-11-18)

This release changes how Icinga Notifications interacts with sources. In
particular, the evaluation of object filters is now performed by the source,
allowing it to use a custom filter syntax and provide more advanced filter
options than previously provided by Icinga Notifications. The previously
built-in Icinga 2 source is removed and replaced by an implementation inside
Icinga DB v1.5.0 that now allows filters based on custom variables, for
example.

This update requires manual steps including a schema upgrade and configuration
changes. Please make sure to read the
[upgrading documentation](https://icinga.com/docs/icinga-notifications/latest/doc/04-Upgrading/#upgrading-to-icinga-notifications-v020)
and follow it carefully.

* Let sources evaluate the object filters from event rules. #324 #356 #354
* Allow setting a custom username for source authentication. #338
* Add /incidents API endpoint allowing sources to query their open incidents. #361
* Fix issues where changes to channels and event rules weren't applied to the
  running configuration correctly. #333 #334
* Move all debug HTTP endpoints to common /debug prefix. #308
* Reorder history events around mute and unmute events. #346
* Extend database schema for new Icinga Notifications Web functionality. #216 #344
* Documentation: Adapt the minimum MariaDB and MySQL versions to the
  requirements of Icinga Notifications Web. #287
* Documentation: Add information how to view additional log message fields with
  systemd journald. #274


## 0.1.1 (2024-07-29)

This is a small bug fix release with the main change being a fix for logging to the systemd journal.

* Logging: Fix missing log message fields in systemd journal (#267)
* HTTP Listener: Don't return 500 Internal Server Error for superfluous events (#251)
* Container Images: include git commit in `icinga-notifications --version` when built on GitHub Actions (#260)


## 0.1.0 (2024-07-25)

Initial release
