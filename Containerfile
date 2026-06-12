# Multi-stage Podman/Containerfile build for the finops-backend HTTP server.
FROM registry.access.redhat.com/ubi9/go-toolset AS build
WORKDIR /src
COPY core/ core/
COPY backend/ backend/
WORKDIR /src/backend
# OpenShift worker nodes are amd64; pin arch so arm64 laptop builds do not produce linux/arm64 binaries.
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /tmp/finops-backend ./cmd/finops-backend

FROM registry.access.redhat.com/ubi9 AS ca-source

FROM registry.access.redhat.com/ubi9/ubi-micro
# ubi-micro omits the system CA bundle; Snowflake TLS needs it for JWT auth.
COPY --from=ca-source /etc/pki /etc/pki
COPY --from=build --chmod=755 /tmp/finops-backend /finops-backend
EXPOSE 8080
USER 1001
ENTRYPOINT ["/finops-backend"]
