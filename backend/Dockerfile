# Lanvory API — multi-stage build.
FROM golang:1.27-alpine AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/lanvory ./cmd/server

FROM golang:1.27-alpine
RUN apk add --no-cache ca-certificates && \
    adduser -D -H -u 10001 lanvory
COPY --from=build /out/lanvory /usr/local/bin/lanvory

USER lanvory
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/lanvory"]
