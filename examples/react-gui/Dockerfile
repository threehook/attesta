# Built with the repo root as context (see Makefile's docker-build-gui) — the pnpm workspace install needs client-lib's package.json too.
FROM node:22-alpine AS builder
WORKDIR /src
RUN corepack enable

COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
COPY client-lib/package.json client-lib/package.json
COPY examples/react-gui/package.json examples/react-gui/package.json
RUN pnpm install --frozen-lockfile

COPY client-lib/ client-lib/
COPY examples/react-gui/ examples/react-gui/
RUN pnpm --filter @zk-puoi/client build

# Empty means same-origin requests, proxied to the backend by nginx.conf; override with --build-arg if bypassing that proxy.
ARG VITE_API_BASE_URL=
ENV VITE_API_BASE_URL=$VITE_API_BASE_URL
RUN pnpm --filter @zk-puoi/react-gui build

FROM nginx:1.27-alpine
COPY --from=builder /src/examples/react-gui/dist /usr/share/nginx/html
COPY examples/react-gui/nginx.conf /etc/nginx/conf.d/default.conf
EXPOSE 80
