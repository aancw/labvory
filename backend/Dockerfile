# Lanvory API — multi-stage build.
FROM golang:1.27-alpine AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/lanvory ./cmd/server

FROM golang:1.27-alpine
# The ARP scan connector opens AF_PACKET sockets: cap_net_raw must be in the
# bounding set (compose cap_add) AND effective for the non-root lanvory user —
# the file capability below provides the latter. /app/backups is pre-created
# with the lanvory owner so a mounted backup volume inherits that ownership
# (scheduled backups default to ./backups relative to the working directory).
RUN apk add --no-cache ca-certificates libcap && \
    adduser -D -H -u 10001 lanvory && \
    mkdir -p /app/backups && chown -R lanvory /app
COPY --from=build /out/lanvory /usr/local/bin/lanvory
RUN setcap cap_net_raw+ep /usr/local/bin/lanvory

WORKDIR /app
USER lanvory
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/lanvory"]
