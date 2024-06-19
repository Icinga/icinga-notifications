# Icinga Notifications | (c) 2023 Icinga GmbH | GPLv2+

FROM docker.io/library/golang as build
ENV CGO_ENABLED 0
COPY . /src/icinga-notifications
WORKDIR /src/icinga-notifications

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    make all

RUN make DESTDIR=/target install

FROM docker.io/library/alpine

COPY --from=build /target /

RUN apk add tzdata

ARG username=notifications
RUN addgroup -g 1000 $username
RUN adduser -u 1000 -H -D -G $username $username
USER $username

EXPOSE 5680
CMD ["/usr/sbin/icinga-notifications", "--config", "/etc/icinga-notifications/config.yml"]
