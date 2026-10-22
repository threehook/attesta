# The wallet of a system (serve-cli.ts). Built with the repo root as context (make docker-build-cicd-wallet): the pnpm workspace install needs the root manifests.
FROM node:22-bookworm-slim
WORKDIR /src
RUN corepack enable

COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
COPY wallet/package.json wallet/package.json
RUN pnpm install --frozen-lockfile --filter @attesta/wallet...

COPY wallet/ wallet/

USER node
WORKDIR /src/wallet
ENV ATTESTA_WALLET_PORT=7100
EXPOSE 7100
CMD ["./node_modules/.bin/tsx", "src/serve-cli.ts"]
