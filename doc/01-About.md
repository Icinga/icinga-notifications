# Icinga Notifications

!!! warning

    This is an early preview version for you to try, but do not use this in production.
    There may still be severe bugs and incompatible changes may happen without any notice.
    At the moment, we don't yet provide any support for this.

Icinga Notifications is a set of components that processes received events from various sources, manages incidents and
forwards notifications to predefined contacts, consisting of:

* The Icinga Notifications daemon (this repository), which receives events and sends notifications.
* An [Icinga Web module](https://github.com/Icinga/icinga-notifications-web) that provides graphical configuration and further processing of the data collected by the daemon.
* Icinga 2 or other sources that provide monitoring events that result in notifications.

## Big Picture

Because Icinga Notifications consists of several components,
this section tries to help understand how these components relate.

First, the Icinga Notifications configuration resides in a SQL database.
It can be conveniently tweaked via Icinga Notifications Web directly from a web browser.
The Icinga Notifications daemon uses this database to read the current configuration.

As in any Icinga setup, all host and service checks are defined in Icinga 2.
By querying the Icinga 2 API, the Icinga Notifications daemon retrieves state changes, acknowledgements, and other events.
These events are stored in the database and are available for further inspection in the Icinga Notifications Web.

Depending on its configuration, the daemon will take further action on these events.
This optionally includes escalations that are sent through a channel plugin.
Each such channel plugin implements a domain-specific transport, e.g., the `email` channel sends emails via SMTP.
When configured, Icinga Notifications will use channel plugins to notify end users or talk to other APIs.

## Installation

To install Icinga Notifications see [Installation](02-Installation.md).

## License

Icinga Notifications and the Icinga Notifications documentation are licensed under the terms of the
GNU General Public License Version 2.
