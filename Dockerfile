# Icinga Notifications | (c) 2023 Icinga GmbH | GPLv2+

FROM docker.io/library/golang as build
ENV CGO_ENABLED 0
COPY . /src/icinga-notifications
WORKDIR /src/icinga-notifications

RUN mkdir bin
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -o bin/ ./cmd/icinga-notifications-daemon

RUN mkdir bin/channel
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -o bin/channel/ ./cmd/channel/...

FROM docker.io/library/alpine

COPY --from=build /src/icinga-notifications/bin/icinga-notifications-daemon /usr/bin/icinga-notifications-daemon
COPY --from=build /src/icinga-notifications/bin/channel /usr/libexec/icinga-notifications/channel

RUN mkdir /etc/icinga-notifications/
COPY config.example.yml /etc/icinga-notifications/config.yml

RUN apk add tzdata

ARG username=notifications
RUN addgroup -g 1000 $username
RUN adduser -u 1000 -H -D -G $username $username
USER $username

EXPOSE 5680
CMD ["/usr/bin/icinga-notifications-daemon", "--config", "/etc/icinga-notifications/config.yml"]
