FROM golang:1.21 AS build
ENV GO111MODULE=on

WORKDIR $GOPATH/src/bootjp/simple_dns
COPY . .
RUN GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -a -o out main.go && cp out /app

FROM gcr.io/distroless/static:latest-arm64
COPY --from=build /app /app/dns
COPY config.yaml /app/config.yaml
WORKDIR /app

EXPOSE 53/udp

CMD ["/app/dns"]
