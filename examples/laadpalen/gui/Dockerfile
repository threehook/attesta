# Built with the repo root as context (see the Makefile's laadpalen target) — the pnpm workspace install needs the root manifests.
FROM node:22-alpine AS builder
WORKDIR /src
RUN corepack enable

COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
COPY examples/laadpalen/gui/package.json examples/laadpalen/gui/package.json
RUN pnpm install --frozen-lockfile --filter @attesta/laadpalen-gui...

COPY examples/laadpalen/gui/ examples/laadpalen/gui/
RUN pnpm --filter @attesta/laadpalen-gui build

FROM nginx:1.27-alpine
COPY --from=builder /src/examples/laadpalen/gui/dist /usr/share/nginx/html
COPY examples/laadpalen/gui/nginx.conf /etc/nginx/conf.d/default.conf
EXPOSE 80
