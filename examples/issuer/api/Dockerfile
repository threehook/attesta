# Built with the repo root as context (see Makefile's docker-build-issuer) — the pnpm workspace install needs the other workspace packages' manifests.
# A glibc base, not alpine: the native Askar library the install script downloads is built for glibc.
FROM node:22-bookworm-slim
WORKDIR /src
RUN corepack enable

COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
COPY wallet/package.json wallet/package.json
COPY examples/issuer/api/package.json examples/issuer/api/package.json
RUN pnpm install --frozen-lockfile --filter @attesta/issuer-api...

COPY wallet/ wallet/
COPY examples/issuer/api/ examples/issuer/api/

USER node
WORKDIR /src/examples/issuer/api
# The signing key comes from the seed, so the issuer's DID survives a restart even though this store does not.
ENV ATTESTA_ISSUER_PORT=4001
ENV ATTESTA_ISSUER_DIR=/tmp/issuer
EXPOSE 4001
CMD ["./node_modules/.bin/tsx", "src/server.ts"]
