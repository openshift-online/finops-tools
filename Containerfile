# Multi-stage Podman/Containerfile build for the finops-backend HTTP server.
FROM registry.access.redhat.com/ubi9/go-toolset AS build
WORKDIR /src
COPY core/ core/
COPY backend/ backend/
WORKDIR /src/backend
# OpenShift worker nodes are amd64; pin arch so arm64 laptop builds do not produce linux/arm64 binaries.
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /tmp/finops-backend ./cmd/finops-backend

FROM registry.access.redhat.com/ubi9 AS ca-source

FROM registry.access.redhat.com/ubi9/ubi-minimal AS health-probe
RUN microdnf install -y curl-minimal && microdnf clean all && \
    mkdir -p /opt/health-probe/lib && \
    cp /usr/bin/curl /opt/health-probe/curl && \
    ldd /usr/bin/curl | awk '/=>/ {print $3}' | xargs -I{} cp {} /opt/health-probe/lib/

FROM registry.access.redhat.com/ubi9/ubi-micro
# ubi-micro omits the system CA bundle; Snowflake TLS needs it for JWT auth.
COPY --from=ca-source /etc/pki /etc/pki
COPY --from=build --chmod=755 /tmp/finops-backend /finops-backend
COPY --from=health-probe /opt/health-probe/curl /usr/bin/curl
COPY --from=health-probe /opt/health-probe/lib/ /lib64/
EXPOSE 8080
USER 1001
ENTRYPOINT ["/finops-backend"]
HEALTHCHECK --interval=10s --timeout=3s --start-period=10s --retries=3 \
  CMD ["/usr/bin/curl", "-fsS", "http://127.0.0.1:8080/livez"]
