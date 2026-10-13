# Built with the repo root as context (see Makefile's docker-build-issuer-gui) — the pnpm workspace install needs the root manifests.
FROM node:22-bookworm-slim
WORKDIR /src
RUN corepack enable

COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
COPY examples/issuer/gui/package.json examples/issuer/gui/package.json
RUN pnpm install --frozen-lockfile --filter @attesta/issuer-gui...

COPY examples/issuer/gui/ examples/issuer/gui/

USER node
WORKDIR /src/examples/issuer/gui
ENV ATTESTA_ISSUER_GUI_LISTEN_PORT=4000
EXPOSE 4000
CMD ["./node_modules/.bin/tsx", "src/server.ts"]
