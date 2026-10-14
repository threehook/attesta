# Built with issuer/api as the context (make docker-build-issuer).
FROM golang:1.27-alpine AS builder
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY cmd/ cmd/
COPY internal/ internal/
RUN CGO_ENABLED=0 go build -o /out/issuer ./cmd/issuer

FROM alpine:3.20
# The CA certificates let the issuer trust a mail server that secures its connection (STARTTLS or TLS).
RUN apk add --no-cache ca-certificates && adduser -D -u 10001 issuer
COPY --from=builder /out/issuer /app/issuer

USER issuer
EXPOSE 4001
ENTRYPOINT ["/app/issuer"]
