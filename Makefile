.PHONY: test lint lint-all lint-install install-hooks build run clean build-backend test-backend \
	podman-build podman-push openshift-apply openshift-restart openshift-refresh

GOPATH_BIN := $(shell go env GOPATH)/bin
GOLANGCI_LINT := $(GOPATH_BIN)/golangci-lint
GOLANGCI_VERSION := v2.12.2
GOLANGCI_PACKAGES := $(shell go list -f '{{.Dir}}/...' -m)
GOLANGCI_MODULE_DIRS := $(shell go list -f '{{.Dir}}' -m)
# Match GitHub Actions only-new-issues: report findings introduced since this ref.
LINT_NEW_FROM ?= origin/main

test:
	go test ./core/... ./cli/... ./backend/...

lint-install:
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_VERSION)

$(GOLANGCI_LINT):
	$(MAKE) lint-install

# golangci-lint cannot use ./... at the go.work root; lint each workspace module.
# Default matches CI (only-new-issues). Use make lint-all to scan the whole tree.
lint: $(GOLANGCI_LINT)
	@for dir in $(GOLANGCI_MODULE_DIRS); do \
		echo "golangci-lint $$dir (new vs $(LINT_NEW_FROM))"; \
		( cd $$dir && $(GOLANGCI_LINT) run --new-from-rev=$(LINT_NEW_FROM) ./... ) || exit $$?; \
	done

lint-all: $(GOLANGCI_LINT)
	$(GOLANGCI_LINT) run $(GOLANGCI_PACKAGES)

# Copy the shared pre-push hook into this clone. Does not change core.hooksPath, so
# existing hooks such as rh-multi-pre-commit stay in place.
install-hooks:
	install -d $(shell git rev-parse --git-path hooks)
	install -m 755 .githooks/pre-push $(shell git rev-parse --git-path hooks)/pre-push

build:
	go build -o bin/finops ./cli/cmd/finops

build-backend:
	go build -o bin/finops-backend ./backend/cmd/finops-backend

test-backend:
	go test ./backend/...

IMAGE ?= images.paas.redhat.com/finops/finops-tools
NAMESPACE ?= finops-team--finops-tools-backend
OPENSHIFT_MANIFESTS ?= \
	deploy/openshift/deployment.yaml \
	deploy/openshift/service.yaml \
	deploy/openshift/route.yaml \
	deploy/openshift/networkpolicy.yaml

podman-build:
	podman build --platform linux/amd64 -t finops-backend:local .
	podman tag finops-backend:local $(IMAGE):latest

podman-push: podman-build
	podman push $(IMAGE):latest

openshift-apply:
	oc apply $(addprefix -f ,$(OPENSHIFT_MANIFESTS)) -n $(NAMESPACE)

openshift-restart:
	oc rollout restart deployment/finops-backend -n $(NAMESPACE)
	oc rollout status deployment/finops-backend -n $(NAMESPACE)

# Rebuild the backend image, push to the cluster registry, apply manifests, and roll out.
openshift-refresh: podman-push openshift-apply openshift-restart

run: build
	./bin/finops --help

clean:
	rm -rf bin dist

# Ad-hoc cross-compile examples:
# GOOS=linux GOARCH=amd64 go build -o bin/finops-linux-amd64 ./cli/cmd/finops
# GOOS=windows GOARCH=amd64 go build -o bin/finops.exe ./cli/cmd/finops
