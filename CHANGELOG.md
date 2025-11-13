# Icinga Notifications Changelog

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
