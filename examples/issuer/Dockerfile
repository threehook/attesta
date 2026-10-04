# Built with the repo root as context (see Makefile's docker-build-issuer) — the pnpm workspace install needs the other workspace packages' manifests.
# A glibc base, not alpine: the native Askar library the install script downloads is built for glibc.
FROM node:22-bookworm-slim
WORKDIR /src
RUN corepack enable

COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
COPY wallet/package.json wallet/package.json
COPY examples/issuer/package.json examples/issuer/package.json
RUN pnpm install --frozen-lockfile --filter @attesta/issuer...

COPY wallet/ wallet/
COPY examples/issuer/ examples/issuer/

USER node
WORKDIR /src/examples/issuer
# The signing key comes from the seed, so the issuer's DID survives a restart even though this store does not.
ENV ATTESTA_ISSUER_PORT=4000
ENV ATTESTA_ISSUER_DIR=/tmp/issuer
EXPOSE 4000
CMD ["./node_modules/.bin/tsx", "src/server.ts"]
