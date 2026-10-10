FROM --platform=$BUILDPLATFORM node:22-alpine AS web
WORKDIR /src/web
COPY web/package*.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.23-bookworm AS backend
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/internal/web/dist ./internal/web/dist
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -buildvcs=false -trimpath -ldflags="-s -w" -o /out/115strm ./cmd/server

FROM gcr.io/distroless/static-debian12
WORKDIR /opt/115strm
LABEL org.opencontainers.image.source="https://github.com/SuYang6/115strm"
COPY --from=backend /out/115strm /opt/115strm/115strm
EXPOSE 8585 8097
VOLUME ["/etc/115strm", "/var/lib/panstr", "/var/log/panstr", "/opt/Media"]
ENTRYPOINT ["/opt/115strm/115strm", "--config", "/etc/115strm/config.yaml"]
