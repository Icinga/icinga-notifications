# Icinga Notifications | (c) 2023 Icinga GmbH | GPLv2+

FROM --platform=$BUILDPLATFORM docker.io/library/golang AS base

WORKDIR /icinga-notifications

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
	go mod download

FROM base AS build

ARG TARGETOS
ARG TARGETARCH

# The source code is mounted read-only, so all build artifacts are written to a separate build directory,
# which is cleaned up once the binaries have been installed into /target.
RUN --mount=type=bind,source=.,target=.,ro \
	--mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH make builddir=/icinga-notifications-build all && \
    make test && \
    make builddir=/icinga-notifications-build DESTDIR=/target install && \
    make builddir=/icinga-notifications-build clean

FROM docker.io/library/alpine AS runtime

COPY --from=build /target /

RUN apk add tzdata

ARG username=notifications
RUN addgroup -g 1000 $username
RUN adduser -u 1000 -H -D -G $username $username
USER $username

EXPOSE 5680
CMD ["/usr/sbin/icinga-notifications", "--database-auto-import"]
