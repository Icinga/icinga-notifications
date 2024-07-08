# Icinga Notifications

!!! warning

    This is an early preview version for you to try, but do not use this in production.
    There may still be severe bugs and incompatible changes may happen without any notice.
    At the moment, we don't yet provide any support for this.

Icinga Notifications is a set of components that processes received events from various sources, manages incidents and
forwards notifications to predefined contacts, consisting of:

* The Icinga Notifications daemon, which receives events and sends notifications.
* The [Icinga Notifications Web](https://icinga.com/docs/icinga-notifications-web/latest/doc/01-About/) module,
  which provides graphical configuration.
* Icinga 2 and other sources that provide monitoring events that result in notifications.

## Big Picture

![Icinga Notifications Architecture](images/icinga-notifications-architecture.png)

Because Icinga Notifications consists of several components,
this section tries to help understand how these components relate.

First, the Icinga Notifications configuration resides in a SQL database.
It can be conveniently tweaked via Icinga Notifications Web directly from a web browser.
The Icinga Notifications daemon uses this database to read the current configuration.

As in any Icinga setup, all host and service checks are defined in Icinga 2.
By querying the Icinga 2 API, the Icinga Notifications daemon retrieves state changes, acknowledgements, and other events.
These events are stored in the database and are available for further inspection in Icinga Notifications Web.
Next to Icinga 2, other notification sources can be configured.

Depending on its configuration, the daemon will take action on these events.
This optionally includes escalations that are sent through a channel plugin.
Each such channel plugin implements a domain-specific transport, e.g., the `email` channel sends emails via SMTP.
When configured, Icinga Notifications will use channel plugins to notify end users or talk to other APIs.

## Available Channels

Icinga Notifications comes with multiple channels out of the box:

* _email_: Email submission via SMTP
* _rocketchat_: Rocket.Chat
* _webhook_: Configurable HTTP/HTTPS queries for your backend

Additional custom channels can be developed independently of Icinga Notifications,
following the [channel specification](10-Channels.md).

## Installation

To install Icinga Notifications see [Installation](02-Installation.md).

## License

Icinga Notifications and the Icinga Notifications documentation are licensed under the terms of the
[GNU General Public License Version 2](https://github.com/Icinga/icinga-notifications/blob/main/LICENSE).
