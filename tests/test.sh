#!/bin/sh

set -eux

ICINGA_TESTING_NOTIFICATIONS_IMAGE="icinga-notifications:latest"
ICINGA_TESTING_ICINGA_NOTIFICATIONS_SCHEMA_PGSQL="$(realpath ../schema/pgsql/schema.sql)"
export ICINGA_TESTING_NOTIFICATIONS_IMAGE
export ICINGA_TESTING_ICINGA_NOTIFICATIONS_SCHEMA_PGSQL

docker build -t "$ICINGA_TESTING_NOTIFICATIONS_IMAGE" ..

test -d ./out && rm -r ./out
mkdir ./out

go test -o ./out/icinga-notifications-test -c .

exec ./out/icinga-notifications-test -icingatesting.debuglog ./out/debug.log -test.v
