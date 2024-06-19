# These variables follow the naming convention from the GNU Make documentation
# but their defaults correspond to the rest of the code (note that changing
# libexecdir here wouldn't affect the default path for the channel plugin
# directory used by the dameon for example).
#
# https://www.gnu.org/software/make/manual/html_node/Directory-Variables.html
prefix     ?= /usr
sbindir    ?= $(prefix)/sbin
libexecdir ?= $(prefix)/libexec
datadir    ?= $(prefix)/share
sysconfdir ?= /etc

all:
	mkdir -p build
	go build -o build/ ./cmd/icinga-notifications
	go build -o build/channel/ ./cmd/channel/...

install:
	@# config
	install -d $(DESTDIR)$(sysconfdir)/icinga-notifications
	install -m644 config.example.yml $(DESTDIR)$(sysconfdir)/icinga-notifications/config.yml
	@# dameon
	install -D build/icinga-notifications $(DESTDIR)$(sbindir)/icinga-notifications
	@# channel plugins
	install -d $(DESTDIR)$(libexecdir)/icinga-notifications/channel
	install build/channel/* $(DESTDIR)$(libexecdir)/icinga-notifications/channel/
	@# database schema
	install -d $(DESTDIR)$(datadir)/icinga-notifications
	cp -rv --no-dereference schema $(DESTDIR)$(datadir)/icinga-notifications
	@# chmod ensures consistent permissions when cp is called with umask != 022
	chmod -R u=rwX,go=rX $(DESTDIR)$(datadir)/icinga-notifications/schema


clean:
	rm -rf build

.PHONY: all install clean
