# Icinga Notifications | (c) 2023 Icinga GmbH | GPLv2+

FROM docker.io/library/golang AS base

WORKDIR /icinga-notifications
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
	go mod download

FROM base AS build

# The source code must be mounted in read-write mode to allow the makefile to temporarily create the build artifacts
# in the source directory. These artifacts will then directly be deleted after the build process is finished.
RUN --mount=type=bind,source=.,target=.,rw \
	--mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 make all && \
    make test && \
    make DESTDIR=/target install && \
    make clean

FROM docker.io/library/alpine AS runtime

COPY --from=build /target /

RUN apk add tzdata

ARG username=notifications
RUN addgroup -g 1000 $username
RUN adduser -u 1000 -H -D -G $username $username
USER $username

EXPOSE 5680
CMD ["/usr/sbin/icinga-notifications"]
