# Developing Icinga Notifications

Building the Icinga Notifications daemon requires a recent version of [Go](https://go.dev/dl/) and
[GNU Make](https://www.gnu.org/software/make/) to be installed.
The required Go version depends on the specified version in the `go.mod` file after the `go` directive.

To fetch the source code,
either an archive can be downloaded from [GitHub release](https://github.com/Icinga/icinga-notifications/releases)
or the repository can be cloned with git.
The latter is highly recommended.

```
git clone https://github.com/Icinga/icinga-notifications.git
cd icinga-notifications
```

To specify destinations for the final install,
consult the variables in the `Makefile` which can be overwritten by environment variables.

In particular, the `prefix` and `sysconfdir` variables should be noted,
as they can be overwritten to point to a directory in the current working directory for testing purposes.
If those are also prefixed with, say `./out`, all build artifacts will end up in that directory.

```
export prefix="out/usr" sysconfdir="out/etc"
```

Now the daemon and all its channels can be built.

```
make
```

Finally, the `Makefile` also allows to perform an installation on the system.
Depending on the installation location, this command might need extended permissions,
so it might need to be executed by the `root` user, through `sudo` or a similar mechanism.

```
make install
```
