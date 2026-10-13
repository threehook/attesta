# Built with the repo root as context (see Makefile's docker-build-issuer) — the pnpm workspace install needs the root manifests.
FROM node:22-bookworm-slim
WORKDIR /src
RUN corepack enable

COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
COPY examples/issuer/pg/package.json examples/issuer/pg/package.json
COPY examples/issuer/login/package.json examples/issuer/login/package.json
RUN pnpm install --frozen-lockfile --filter @attesta/issuer-login...

COPY examples/issuer/pg/ examples/issuer/pg/
COPY examples/issuer/login/ examples/issuer/login/

USER node
WORKDIR /src/examples/issuer/login
ENV ATTESTA_LOGIN_PORT=4002
EXPOSE 4002
CMD ["./node_modules/.bin/tsx", "src/server.ts"]
