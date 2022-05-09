FROM golang:1.17 as builder

COPY . /demo

WORKDIR /demo
RUN go build -o bin/demo-controller main.go

FROM debian:stretch

RUN mkdir -p /demo && \
    chown -R nobody:nogroup /demo

COPY --from=builder \
  /demo/bin/demo-controller \
  /usr/local/bin/demo-controller

USER        nobody
WORKDIR     /demo
ENTRYPOINT  ["demo-controller"]
