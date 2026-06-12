# Multi-stage Podman/Containerfile build for the finops-backend HTTP server.
FROM golang:1.25 AS build
WORKDIR /src
COPY core/ core/
COPY backend/ backend/
WORKDIR /src/backend
# OpenShift worker nodes are amd64; pin arch so arm64 laptop builds do not produce linux/arm64 binaries.
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /finops-backend ./cmd/finops-backend

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /finops-backend /finops-backend
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/finops-backend"]
