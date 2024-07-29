# Icinga Notifications Changelog

## 0.1.1 (2024-07-29)

This is a small bug fix release with the main change being a fix for logging to the systemd journal.

* Logging: Fix missing log message fields in systemd journal (#267)
* HTTP Listener: Don't return 500 Internal Server Error for superfluous events (#251)
* Container Images: include git commit in `icinga-notifications --version` when built on GitHub Actions (#260)


## 0.1.0 (2024-07-25)

Initial release
