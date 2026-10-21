FROM golang:1.25 AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/ai-chatbot ./cmd/server

FROM debian:bookworm-slim
# CA certificates are needed for HTTPS calls to Sarvam and Claude.
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY --from=builder /app/ai-chatbot .
EXPOSE 5090
CMD ["/app/ai-chatbot"]
