# Built with examples/laadpalen/api as the context (see the Makefile's docker-build-laadpalen-api); the module needs go.mod and go.sum for enforcer-go.
FROM golang:1.27-alpine AS builder
WORKDIR /src

COPY go.mod go.sum ./
COPY *.go ./
RUN go mod download
RUN CGO_ENABLED=0 go build -o /out/laadpalen-api .

FROM alpine:3.20
RUN adduser -D -u 10001 laadpalen
COPY --from=builder /out/laadpalen-api /app/laadpalen-api

USER laadpalen
EXPOSE 8787
ENTRYPOINT ["/app/laadpalen-api"]
