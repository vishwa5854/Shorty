# syntax=docker/dockerfile:1

FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/shorty .

FROM alpine:3.22
RUN adduser -D -u 10001 shorty \
 && mkdir -p /data \
 && chown shorty:shorty /data
COPY --from=build /out/shorty /usr/local/bin/shorty
USER shorty
ENV SHORTY_ADDR=:8080 \
    SHORTY_DB=/data/shorty.db
EXPOSE 8080
VOLUME ["/data"]
ENTRYPOINT ["shorty"]
