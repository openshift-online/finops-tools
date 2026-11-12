.PHONY: test lint lint-all lint-install install-hooks build run clean build-backend test-backend \
	podman-build podman-push openshift-check-login openshift-apply openshift-restart openshift-refresh

GOPATH_BIN := $(shell go env GOPATH)/bin
GOLANGCI_LINT := $(GOPATH_BIN)/golangci-lint
GOLANGCI_VERSION := v2.12.2
GOLANGCI_PACKAGES := $(shell go list -f '{{.Dir}}/...' -m)
GOLANGCI_MODULE_DIRS := $(shell go list -f '{{.Dir}}' -m)
# .SHELLSTATUS is GNU Make 4.2+; older make expands it empty.
GOLANGCI_MODULE_DIRS_STATUS := $(.SHELLSTATUS)
# Event-specific baseline for only-new-issues. CI uses --new-from-patch (PR/push
# diff). Override to match an event, e.g.:
#   make lint LINT_NEW_FROM=<pr-base-sha>       # pull_request
#   make lint LINT_NEW_FROM=<push-before-sha>   # push
# Default origin/main is resolved to a merge-base with HEAD (PR-style patch).
LINT_NEW_FROM ?= origin/main
# Match CI actions/setup-go go-version: "1.25". A newer host Go (e.g. Homebrew
# 1.27) can break golangci-lint typecheck on stdlib export data.
GOTOOLCHAIN_LINT ?= go1.25.5

test:
	go test ./core/... ./cli/... ./backend/...

lint-install:
	GOTOOLCHAIN=$(GOTOOLCHAIN_LINT) go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_VERSION)

$(GOLANGCI_LINT):
	$(MAKE) lint-install

# golangci-lint cannot use ./... at the go.work root; lint each workspace module.
# Default matches CI (only-new-issues). Use make lint-all to scan the whole tree.
lint: $(GOLANGCI_LINT)
	@if [ -n "$(GOLANGCI_MODULE_DIRS_STATUS)" ] && [ "$(GOLANGCI_MODULE_DIRS_STATUS)" != 0 ]; then \
		echo "error: go list -m failed with status $(GOLANGCI_MODULE_DIRS_STATUS)" >&2; \
		exit 1; \
	fi; \
	if [ -z "$(GOLANGCI_MODULE_DIRS)" ]; then \
		echo "error: go list -m discovered no modules" >&2; \
		exit 1; \
	fi; \
	ref="$(LINT_NEW_FROM)"; \
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
		( cd $$dir && GOTOOLCHAIN=$(GOTOOLCHAIN_LINT) $(GOLANGCI_LINT) run --new-from-rev=$$baseline ./... ) || exit $$?; \
	done

lint-all: $(GOLANGCI_LINT)
	GOTOOLCHAIN=$(GOTOOLCHAIN_LINT) $(GOLANGCI_LINT) run $(GOLANGCI_PACKAGES)

# Copy the shared pre-push hook into this clone. Does not change core.hooksPath, so
# existing hooks such as rh-multi-pre-commit stay in place. A different pre-push
# already in this clone is moved to pre-push.local and chained; installation is
# refused if that would overwrite a distinct pre-push.local.
install-hooks:
	@set -e; \
	hooks_dir=$$(git rev-parse --git-path hooks) || exit $$?; \
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
# OPENSHIFT_ENV=preprod (default) targets mpp-w2-preprod; OPENSHIFT_ENV=prod targets mpp-e1-prod.
OPENSHIFT_ENV ?= preprod

ifeq ($(OPENSHIFT_ENV),prod)
OPENSHIFT_CLUSTER ?= prod-mpp-spoke-aws-us-east-1
OPENSHIFT_API ?= https://api.mpp-e1-prod.9e4s.p1.openshiftapps.com:6443
OPENSHIFT_CONSOLE ?= https://console-openshift-console.apps.mpp-e1-prod.9e4s.p1.openshiftapps.com
OPENSHIFT_MANIFEST_DIR ?= deploy/openshift-prod
else ifeq ($(OPENSHIFT_ENV),preprod)
OPENSHIFT_CLUSTER ?= mpp-w2-preprod
OPENSHIFT_API ?= https://api.mpp-w2-preprod.cfln.p1.openshiftapps.com:6443
OPENSHIFT_CONSOLE ?= https://console-openshift-console.apps.mpp-w2-preprod.cfln.p1.openshiftapps.com
OPENSHIFT_MANIFEST_DIR ?= deploy/openshift
else
$(error OPENSHIFT_ENV must be preprod or prod (got $(OPENSHIFT_ENV)))
endif

OPENSHIFT_MANIFESTS ?= \
	$(OPENSHIFT_MANIFEST_DIR)/deployment.yaml \
	$(OPENSHIFT_MANIFEST_DIR)/service.yaml \
	$(OPENSHIFT_MANIFEST_DIR)/route.yaml \
	$(OPENSHIFT_MANIFEST_DIR)/networkpolicy.yaml

podman-build:
	podman build --platform linux/amd64 -t finops-backend:local .
	podman tag finops-backend:local $(IMAGE):latest

podman-push: podman-build
	podman push $(IMAGE):latest

# Fail fast (or start an interactive web login) when oc credentials are missing/expired.
openshift-check-login:
	@if ! command -v oc >/dev/null 2>&1; then \
		echo "error: oc CLI not found; install the OpenShift CLI before deploying" >&2; \
		exit 1; \
	fi; \
	if oc whoami --server="$(OPENSHIFT_API)" >/dev/null 2>&1; then \
		echo "openshift ($(OPENSHIFT_ENV)): logged in as $$(oc whoami --server="$(OPENSHIFT_API)") ($$(oc whoami --show-server 2>/dev/null || echo $(OPENSHIFT_API)))"; \
		exit 0; \
	fi; \
	echo "openshift: not logged in to $(OPENSHIFT_CLUSTER) (credentials missing or expired)." >&2; \
	echo "" >&2; \
	echo "Log in, then re-run this make target:" >&2; \
	echo "  1. Open $(OPENSHIFT_CONSOLE)" >&2; \
	echo "  2. User menu → Copy login command → paste the oc login --token=... line" >&2; \
	echo "  Or: oc login --web --server=$(OPENSHIFT_API)" >&2; \
	echo "" >&2; \
	if [ -t 0 ] && [ -t 1 ]; then \
		echo "Attempting interactive web login..." >&2; \
		oc login --web --server="$(OPENSHIFT_API)" || { \
			echo "error: web login failed; use Copy login command from the console" >&2; \
			exit 1; \
		}; \
		if ! oc whoami --server="$(OPENSHIFT_API)" >/dev/null 2>&1; then \
			echo "error: still not logged in after oc login --web" >&2; \
			exit 1; \
		fi; \
		echo "openshift ($(OPENSHIFT_ENV)): logged in as $$(oc whoami --server="$(OPENSHIFT_API)")"; \
	else \
		echo "error: non-interactive session; log in manually before make openshift-*" >&2; \
		exit 1; \
	fi

openshift-apply: openshift-check-login
	oc apply $(addprefix -f ,$(OPENSHIFT_MANIFESTS)) -n $(NAMESPACE) --server="$(OPENSHIFT_API)"

openshift-restart: openshift-check-login
	oc rollout restart deployment/finops-backend -n $(NAMESPACE) --server="$(OPENSHIFT_API)"
	oc rollout status deployment/finops-backend -n $(NAMESPACE) --server="$(OPENSHIFT_API)"

# Rebuild the backend image, push to the cluster registry, apply manifests, and roll out.
# Checks oc login before building so expired credentials fail before a long image build.
# Recipe steps (not sibling prerequisites) keep order under make -j.
# Example prod: make openshift-refresh OPENSHIFT_ENV=prod
openshift-refresh: openshift-check-login
	$(MAKE) podman-push
	$(MAKE) openshift-apply OPENSHIFT_ENV=$(OPENSHIFT_ENV)
	$(MAKE) openshift-restart OPENSHIFT_ENV=$(OPENSHIFT_ENV)

run: build
	./bin/finops --help

clean:
	rm -rf bin dist

# Ad-hoc cross-compile examples:
# GOOS=linux GOARCH=amd64 go build -o bin/finops-linux-amd64 ./cli/cmd/finops
# GOOS=windows GOARCH=amd64 go build -o bin/finops.exe ./cli/cmd/finops
