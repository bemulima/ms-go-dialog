FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o ms-dialog-service ./cmd/ms-dialog-service

FROM alpine:3.19
WORKDIR /app
COPY --from=builder /app/ms-dialog-service .
EXPOSE 8080
CMD ["/app/ms-dialog-service"]
