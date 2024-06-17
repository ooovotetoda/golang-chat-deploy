FROM golang:1.22 AS builder

WORKDIR /app

COPY go.mod go.sum ./

RUN go mod download

COPY . .

RUN go build -o websocket-server .

FROM ubuntu:latest

WORKDIR /root/

RUN apt-get update && apt-get install -y ca-certificates

COPY --from=builder /app/websocket-server .

RUN ls -la

EXPOSE 8080

CMD ["./websocket-server"]
