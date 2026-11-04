# syntax=docker/dockerfile:1
FROM --platform=$BUILDPLATFORM node:24-alpine AS frontend
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS backend
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /src/web/assets/dist ./web/assets/dist
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/wotm ./cmd/wotm

FROM alpine:3.23
RUN apk add --no-cache ca-certificates tzdata && addgroup -g 10001 wotm && adduser -D -u 10001 -G wotm wotm && mkdir /data && chown wotm:wotm /data
COPY --from=backend /out/wotm /usr/local/bin/wotm
USER 10001:10001
WORKDIR /data
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 CMD wget -q -O /dev/null http://127.0.0.1:8080/api/health || exit 1
ENTRYPOINT ["/usr/local/bin/wotm"]
CMD ["-config", "/data/config.yaml", "-listen", ":8080"]
