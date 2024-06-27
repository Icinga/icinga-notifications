# These variables follow the naming convention from the GNU Make documentation
# but their defaults correspond to the rest of the code (note that changing
# libexecdir here wouldn't affect the default path for the channel plugin
# directory used by the daemon for example).
#
# https://www.gnu.org/software/make/manual/html_node/Directory-Variables.html
prefix     ?= /usr
sbindir    ?= $(prefix)/sbin
libexecdir ?= $(prefix)/libexec
datadir    ?= $(prefix)/share
sysconfdir ?= /etc

all: pkg = github.com/icinga/icinga-notifications/internal
all:
	mkdir -p build
	go build \
		-o build/ \
		-ldflags "-X '$(pkg).LibExecDir=$(libexecdir)' -X '$(pkg).SysConfDir=$(sysconfdir)'" \
		./cmd/icinga-notifications
	go build -o build/channels/ ./cmd/channels/...

test:
	go test ./...

install:
	@# config
	install -d $(DESTDIR)$(sysconfdir)/icinga-notifications
	install -m644 config.example.yml $(DESTDIR)$(sysconfdir)/icinga-notifications/config.yml
	@# dameon
	install -D build/icinga-notifications $(DESTDIR)$(sbindir)/icinga-notifications
	@# channels
	install -d $(DESTDIR)$(libexecdir)/icinga-notifications/channels
	install build/channels/* $(DESTDIR)$(libexecdir)/icinga-notifications/channels/
	@# database schema
	install -d $(DESTDIR)$(datadir)/icinga-notifications
	cp -rv --no-dereference schema $(DESTDIR)$(datadir)/icinga-notifications
	@# chmod ensures consistent permissions when cp is called with umask != 022
	chmod -R u=rwX,go=rX $(DESTDIR)$(datadir)/icinga-notifications/schema

clean:
	rm -rf build

.PHONY: all test install clean
