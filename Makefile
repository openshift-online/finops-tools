.PHONY: test lint lint-all lint-install install-hooks build run clean build-backend test-backend \
	podman-build podman-push openshift-apply openshift-restart openshift-refresh

GOPATH_BIN := $(shell go env GOPATH)/bin
GOLANGCI_LINT := $(GOPATH_BIN)/golangci-lint
GOLANGCI_VERSION := v2.12.2
GOLANGCI_PACKAGES := $(shell go list -f '{{.Dir}}/...' -m)
GOLANGCI_MODULE_DIRS := $(shell go list -f '{{.Dir}}' -m)
# Event-specific baseline for only-new-issues. CI uses --new-from-patch (PR/push
# diff). Override to match an event, e.g.:
#   make lint LINT_NEW_FROM=<pr-base-sha>       # pull_request
#   make lint LINT_NEW_FROM=<push-before-sha>   # push
# Default origin/main is resolved to a merge-base with HEAD (PR-style patch).
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
	@ref="$(LINT_NEW_FROM)"; \
	if [ -z "$$ref" ]; then \
		echo "error: LINT_NEW_FROM is empty; set it to a git ref or SHA (CI --new-from-patch baseline)" >&2; \
		exit 1; \
	fi; \
	if ! git rev-parse --verify --quiet "$$ref^{commit}" >/dev/null; then \
		echo "error: lint baseline $$ref does not exist; fetch origin/main or set LINT_NEW_FROM to the CI event SHA" >&2; \
		exit 1; \
	fi; \
	if [ "$$(git rev-parse "$$ref^{commit}")" = "$$(git rev-parse HEAD)" ]; then \
		echo "error: lint baseline $$ref equals HEAD; set LINT_NEW_FROM to the push-before SHA or PR base (CI --new-from-patch)" >&2; \
		exit 1; \
	fi; \
	baseline=$$(git merge-base HEAD "$$ref") || { \
		echo "error: no merge-base between HEAD and $$ref; set LINT_NEW_FROM to a reachable CI event SHA" >&2; \
		exit 1; \
	}; \
	if [ "$$baseline" = "$$(git rev-parse HEAD)" ]; then \
		echo "error: HEAD is not ahead of lint baseline $$ref; fetch a current base or set LINT_NEW_FROM to the CI event SHA" >&2; \
		exit 1; \
	fi; \
	for dir in $(GOLANGCI_MODULE_DIRS); do \
		echo "golangci-lint $$dir (new vs $$baseline)"; \
		( cd $$dir && $(GOLANGCI_LINT) run --new-from-rev=$$baseline ./... ) || exit $$?; \
	done

lint-all: $(GOLANGCI_LINT)
	$(GOLANGCI_LINT) run $(GOLANGCI_PACKAGES)

# Copy the shared pre-push hook into this clone. Does not change core.hooksPath, so
# existing hooks such as rh-multi-pre-commit stay in place. A different pre-push
# already in this clone is moved to pre-push.local and chained; installation is
# refused if that would overwrite a distinct pre-push.local.
install-hooks:
	@hooks_dir=$$(git rev-parse --git-path hooks); \
	src=.githooks/pre-push; \
	dest=$$hooks_dir/pre-push; \
	saved=$$hooks_dir/pre-push.local; \
	install -d "$$hooks_dir"; \
	if [ ! -e "$$dest" ] && [ ! -L "$$dest" ]; then \
		install -m 755 "$$src" "$$dest"; \
		echo "install-hooks: installed $$dest"; \
	elif cmp -s "$$src" "$$dest"; then \
		echo "install-hooks: $$dest already up to date"; \
	elif grep -qE 'finops-tools: pre-push|# Bypass:  git push --no-verify' "$$dest" 2>/dev/null; then \
		install -m 755 "$$src" "$$dest"; \
		echo "install-hooks: updated $$dest"; \
	else \
		if { [ -e "$$saved" ] || [ -L "$$saved" ]; } && ! cmp -s "$$dest" "$$saved"; then \
			echo "error: $$dest already exists and differs from $$src" >&2; \
			echo "error: $$saved also exists; refusing to overwrite either hook" >&2; \
			exit 1; \
		fi; \
		mv "$$dest" "$$saved"; \
		install -m 755 "$$src" "$$dest"; \
		echo "install-hooks: preserved existing hook as $$saved (both will run on push)"; \
	fi

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
