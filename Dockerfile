FROM golang:1.22-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o shortener ./cmd/url-shortener

FROM alpine:latest

WORKDIR /root/

COPY --from=builder /app/shortener .

EXPOSE 8080

CMD ["./shortener"]
