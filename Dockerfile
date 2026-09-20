# Both stages pin the same Alpine minor (3.24) explicitly — the base image
# for the runtime stage must match the build stage's musl exactly, since
# mattn/go-sqlite3 is cgo and the resulting binary links against it. The Go
# version here must also stay >= go.mod's `go` directive, or the build
# stage falls back to downloading that toolchain at build time.
FROM golang:1.26-alpine3.24 AS build
RUN apk add --no-cache gcc musl-dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# mattn/go-sqlite3 is a cgo binding to the real SQLite C library, so
# CGO_ENABLED=1 and a C toolchain (gcc/musl-dev above) are required.
RUN CGO_ENABLED=1 go build -o /out/gunpla-collector ./cmd/gunpla-collector

FROM alpine:3.24
RUN apk add --no-cache busybox-suid tzdata ca-certificates
# Bake in Amsterdam local time so crond (which schedules jobs on the
# system's wall clock) runs at 18:00/18:15 local time year-round, including
# across the CET/CEST DST switch — not a fixed UTC offset.
ENV TZ=Europe/Amsterdam
RUN cp /usr/share/zoneinfo/$TZ /etc/localtime && echo "$TZ" > /etc/timezone
COPY --from=build /out/gunpla-collector /usr/local/bin/gunpla-collector
COPY crontab /etc/crontabs/root
COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
RUN chmod +x /usr/local/bin/docker-entrypoint.sh && mkdir -p /data

# The entrypoint snapshots the container's env for cron jobs to source,
# then execs crond in the foreground as PID 1.
ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]
