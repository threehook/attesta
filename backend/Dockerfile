FROM golang:1.27-alpine AS builder
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -o /out/attesta ./cmd/attesta

FROM alpine:3.20
RUN adduser -D -u 10001 attesta
WORKDIR /app

COPY --from=builder /out/attesta ./attesta

ENV ATTESTA_ADDR=:8080

USER attesta
EXPOSE 8080
ENTRYPOINT ["./attesta"]
