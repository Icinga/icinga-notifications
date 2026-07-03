# Distributed Setups

Icinga Notifications supports running multiple daemon instances against a single shared database.
This allows for high-availability setups where instances share the processing load
and the remaining instances continue operating if one becomes unavailable.

## Architecture

All instances share the same database for both configuration and event processing.
When an event is received via the [HTTP API](20-HTTP-API.md),
the receiving instance writes it to a queue table in the database rather than processing it immediately.
A background worker in each instance continuously polls this queue and claims events for processing.

The database ensures that each event is processed exactly once across all running instances.
There is no leader election; all instances are equal and contribute to the shared workload.

Although database queries ensure that no two instances operate on identical objects, submission time is important.
Therefore, ensure that all nodes have identical system times, e.g., via NTP.

## Configuration

To run additional instances, install Icinga Notifications on another host and point it at the same database.
No additional configuration is required beyond what is described in [Configuration](03-Configuration.md).

Each instance must be able to reach the database and requires any configured notification channels to be installed.
The HTTP API of each instance operates independently.
If you place a load balancer in front of multiple instances,
incoming events will be distributed across them and processed from the shared queue.

## Corrupt Queue Entries

The event queue is designed to be an internal API, where no external process is allowed to insert events.
Thus, it is expected to only contain valid data.

Nevertheless, in the unlikely case that corrupted data crept into the `event_queue` table,
Icinga Notifications will log an error and mark the event as failed in the database.
The log contains a reference to the invalid `event_queue` row including its `id`.
This allows inspecting the entry in the relational database and eventually fixing or deleting it.
