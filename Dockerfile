FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /storavix ./cmd/storavix
FROM alpine:3.21
RUN apk add --no-cache ca-certificates && addgroup -S storavix && adduser -S -G storavix -u 10001 storavix
COPY --from=build /storavix /usr/local/bin/storavix
RUN mkdir -p /data && chown 10001:10001 /data
USER storavix
ENV STORAVIX_DATA_DIR=/data STORAVIX_LISTEN=0.0.0.0:8080
EXPOSE 8080
VOLUME ["/data"]
ENTRYPOINT ["/usr/local/bin/storavix"]
