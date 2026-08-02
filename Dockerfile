# Build stage
FROM golang:1.26-trixie AS builder

WORKDIR /build

COPY go.mod go.sum ./
COPY third_party/go-haiku ./third_party/go-haiku
RUN go mod download

COPY . .
RUN CGO_ENABLED=1 go build -trimpath -ldflags="-s -w" -o bot .
RUN CGO_ENABLED=1 go build -trimpath -ldflags="-s -w" -o migrate ./cmd/migrate

# Runtime stage
FROM gcr.io/distroless/base-debian13:nonroot

WORKDIR /app
COPY --from=builder /build/bot /app/bot
COPY --from=builder /build/migrate /app/migrate

EXPOSE 9090

ENTRYPOINT ["/app/bot"]
