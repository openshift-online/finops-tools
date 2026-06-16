.PHONY: test build run clean build-backend test-backend podman-build podman-push \
	openshift-apply openshift-restart openshift-refresh

test:
	go test ./core/... ./cli/... ./backend/...

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
	./bin/finops demo hello

clean:
	rm -rf bin dist

# Ad-hoc cross-compile examples:
# GOOS=linux GOARCH=amd64 go build -o bin/finops-linux-amd64 ./cli/cmd/finops
# GOOS=windows GOARCH=amd64 go build -o bin/finops.exe ./cli/cmd/finops
