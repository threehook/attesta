# Built with the repo root as context (see Makefile's docker-build), not backend/ — this image needs files from
# client-lib too (the circom-exported verification key), and COPY can't reach outside its build context otherwise.
FROM golang:1.27-alpine AS builder
WORKDIR /src

COPY backend/go.mod backend/go.sum ./
RUN go mod download

COPY backend/ .
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
COPY backend/policies ./policies
COPY client-lib/circuits/cubic/build/verification_key.json ./circuits/cubic/verification_key.json

ENV GNOROOT=/app/gnoroot
# ^ points gnovm at the stdlib/examples folders copied in above.
ENV ZKPUOI_ADDR=:8080
ENV ZKPUOI_POLICIES_DIR=/app/policies
ENV ZKPUOI_VERIFICATION_KEY=/app/circuits/cubic/verification_key.json

USER zkpuoi
EXPOSE 8080
ENTRYPOINT ["./zk-puoi"]
