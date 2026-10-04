# Built with the repo root as context (see Makefile's docker-build-gui) — the pnpm workspace install needs client-lib's package.json too.
FROM node:22-alpine AS builder
WORKDIR /src
RUN corepack enable

COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
COPY client-lib/package.json client-lib/package.json
COPY examples/simple-gui/package.json examples/simple-gui/package.json
RUN pnpm install --frozen-lockfile --filter @attesta/simple-gui...

COPY client-lib/ client-lib/
COPY examples/simple-gui/ examples/simple-gui/
RUN pnpm --filter @attesta/client build

# Empty means same-origin requests, proxied to the backend by nginx.conf; override with --build-arg if bypassing that proxy.
ARG VITE_API_BASE_URL=
ENV VITE_API_BASE_URL=$VITE_API_BASE_URL
RUN pnpm --filter @attesta/simple-gui build

FROM nginx:1.27-alpine
COPY --from=builder /src/examples/simple-gui/dist /usr/share/nginx/html
COPY examples/simple-gui/nginx.conf /etc/nginx/conf.d/default.conf
EXPOSE 80
