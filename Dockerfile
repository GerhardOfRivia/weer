FROM node:24-bookworm-slim AS frontend
WORKDIR /src/web
RUN npm install -g pnpm@11.25.0
COPY web/package.json web/pnpm-lock.yaml web/pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile
COPY web/ ./
RUN pnpm build

FROM golang:1.27-bookworm AS backend
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ cmd/
COPY internal/ internal/
RUN CGO_ENABLED=0 go build -trimpath -o /weer ./cmd/weer

FROM python:3.12-slim-bookworm
RUN useradd --uid 10001 --create-home weer && mkdir -p /data /app && chown weer:weer /data /app
WORKDIR /app
COPY processor/requirements.lock /app/processor/requirements.lock
RUN pip install --no-cache-dir -r processor/requirements.lock
COPY processor/ /app/processor/
RUN pip install --no-cache-dir --no-deps /app/processor
COPY --from=backend /weer /usr/local/bin/weer
COPY --from=frontend /src/web/dist /app/web/dist
COPY configs/docker.yaml /app/weer.yaml
ENV MPLCONFIGDIR=/data/mpl-cache
USER 10001:10001
EXPOSE 8080
VOLUME /data
ENTRYPOINT ["weer"]
CMD ["serve", "--config", "/app/weer.yaml"]
