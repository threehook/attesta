FROM golang:1.27-alpine AS builder
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -o /out/zk-puoi ./cmd/zk-puoi

# gnovm reads its standard library and examples from disk (the GNOROOT dir, set below), so copy just those two
# folders out of the downloaded gno module into the image.
RUN GNODIR=$(go list -m -f '{{.Dir}}' github.com/gnolang/gno) \
    && mkdir -p /out/gnoroot/gnovm /out/gnoroot/examples \
    && cp -r "$GNODIR/gnovm/stdlibs" /out/gnoroot/gnovm/stdlibs \
    && cp -r "$GNODIR/examples" /out/gnoroot/examples

FROM alpine:3.20
RUN adduser -D -u 10001 zkpuoi
WORKDIR /app

COPY --from=builder /out/zk-puoi ./zk-puoi
COPY --from=builder /out/gnoroot ./gnoroot

ENV GNOROOT=/app/gnoroot
# ^ points gnovm at the stdlib/examples folders copied in above.
ENV ZKPUOI_ADDR=:8080

USER zkpuoi
EXPOSE 8080
ENTRYPOINT ["./zk-puoi"]
