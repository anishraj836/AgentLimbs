FROM golang:1.22-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o /app/indexer-service ./indexer-service/main.go

FROM alpine:latest

WORKDIR /app

COPY --from=builder /app/indexer-service .

CMD ["./indexer-service"]
