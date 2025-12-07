FROM golang:1.25-alpine AS builder

WORKDIR /app

COPY go.mod .
COPY main.go .

RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o /app/ssh-wol-proxy .

FROM alpine:3.22.2

WORKDIR /app

COPY --from=builder /app/ssh-wol-proxy .

EXPOSE 2222

CMD ["./ssh-wol-proxy"]
