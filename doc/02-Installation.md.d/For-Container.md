# Installing Icinga Notifications in Containers

The Icinga Notifications container image ships the daemon, all built-in channels and the database schema files.
The image is published to Docker Hub as [`icinga/icinga-notifications`](https://hub.docker.com/r/icinga/icinga-notifications)
and is also available from the GitHub Container Registry (GHCR) as `ghcr.io/icinga/icinga-notifications`.

Icinga Notifications keeps all of its state in the database. This allows you to run multiple containers against the
same database for load sharing and high availability, as described in
the [Icinga Notifications Distributed Setups documentation].

## Prerequisites

- A container runtime such as Docker or Podman.
- A database server as described in the [Setting up Icinga Notifications Database documentation].
  The schema is imported automatically on the first start, see [Database Schema](#database-schema).
- [Icinga Notifications Web], which is used to manage sources, channels, contacts and rules.

The examples below use Docker, but Podman works the same way by replacing `docker` with `podman`.

## Container Network

Icinga DB submits events to the HTTP API of Icinga Notifications, so both containers must share a network.
If you already run your Icinga stack in containers, attach Icinga Notifications to the existing network.
Otherwise, create a new one:

```bash
docker network create icinga
```

## Database Schema

The schema files are part of the image and are located in `/usr/share/icinga-notifications/schema`.
You do not need to import them manually. The container starts the daemon with the `--database-auto-import` flag,
so if the database does not contain an Icinga Notifications schema yet, the daemon imports it on startup.

Schema checks and imports are serialized by a database lock, so several containers can be started against the same
empty database at once without importing the schema twice. The schema is only imported automatically if the database
is empty. Upgrades of an existing schema must be done manually as described in the
[Icinga Notifications Database Schema Upgrades documentation].

## Configuration

The image contains the example configuration file at `/etc/icinga-notifications/config.yml`.
You can configure Icinga Notifications either through environment variables or by mounting your own configuration file.
Environment variables take precedence over the values from the configuration file.
All available options and their environment variable names are listed in the [Icinga Notifications configuration documentation].

### Configuration File

Alternatively, mount your own configuration file over the one in the image:

```bash
--volume /path/to/your/config.yml:/etc/icinga-notifications/config.yml:ro
```

The container runs as the unprivileged user `notifications` with UID and GID `1000`.
Make sure that any mounted file, including secrets, TLS certificates and keys, is readable by this user.

### Listener Address

By default, the HTTP listener only binds to `localhost:5680`. Inside a container, this means that other containers,
such as Icinga DB, cannot reach it. Always set the listener address to listen on all interfaces:

```bash
ICINGA_NOTIFICATIONS_LISTENER_ADDRESS=:5680
```

## Running the Container

Start Icinga Notifications with the following command, adjusting the database settings to your setup.
On the first start, the database schema is imported automatically, as described in [Database Schema](#database-schema):

```bash
docker run -d \
  --name icinga-notifications \
  --network icinga \
  -e ICINGA_NOTIFICATIONS_LISTENER_ADDRESS=:5680 \
  -e ICINGA_NOTIFICATIONS_DATABASE_TYPE=mysql \
  -e ICINGA_NOTIFICATIONS_DATABASE_HOST=mysql \
  -e ICINGA_NOTIFICATIONS_DATABASE_DATABASE=notifications \
  -e ICINGA_NOTIFICATIONS_DATABASE_USER=notifications \
  -e ICINGA_NOTIFICATIONS_DATABASE_PASSWORD=CHANGEME \
  -e TZ=Europe/Berlin \
  icinga/icinga-notifications
```

Publishing port `5680` with `-p 5680:5680` is only necessary if sources outside the container network need to reach
the HTTP API.

The image contains the time zone database, so you can set the container's time zone with the `TZ` variable.
Make sure it matches your expectations, as timeperiods and schedules depend on it.

## Connecting Icinga DB

Add an Icinga DB source in Icinga Notifications Web and configure the
[`notifications` option](https://icinga.com/docs/icinga-db/latest/doc/03-Configuration/#notifications-configuration)
of Icinga DB with the credentials of that source.
Set the `url` to the container name of Icinga Notifications, e.g., `http://icinga-notifications:5680/`.

## Channels

All built-in channels are already part of the image, and the default `channels_dir` points to them.
To use custom channels, mount a directory that contains them and set `ICINGA_NOTIFICATIONS_CHANNELS_DIR` to its path.
Keep in mind that this replaces the default directory, so it must also contain the built-in channels you want to use.
Custom channels must be executable by the `notifications` user and built for the architecture of the container.

## Building Your Own Image

To build your own image, clone the [Icinga Notifications repository](https://github.com/Icinga/icinga-notifications)
and build the image from the `Containerfile`:

```bash
git clone https://github.com/Icinga/icinga-notifications.git
cd icinga-notifications
docker build -t icinga-notifications -f Containerfile .
```

[Icinga Notifications configuration documentation]: https://icinga.com/docs/icinga-notifications/latest/doc/03-Configuration/
[Icinga Notifications Distributed Setups documentation]: https://icinga.com/docs/icinga-notifications/latest/doc/05-Distributed-Setups/
[Setting up Icinga Notifications Database documentation]: https://icinga.com/docs/icinga-notifications/latest/doc/02-Installation/Debian/#setting-up-the-database
[Icinga Notifications Database Schema Upgrades documentation]: https://icinga.com/docs/icinga-notifications/latest/doc/04-Upgrading/#database-schema-upgrades
[Icinga Notifications Web]: https://icinga.com/docs/icinga-notifications-web/latest/doc/02-Installation/
