.PHONY: test build run clean build-backend test-backend podman-build podman-push

test:
	go test ./core/... ./cli/... ./backend/...

build:
	go build -o bin/finops ./cli/cmd/finops

build-backend:
	go build -o bin/finops-backend ./backend/cmd/finops-backend

test-backend:
	go test ./backend/...

IMAGE ?= images.paas.redhat.com/finops/finops-tools

podman-build:
	podman build --platform linux/amd64 -t finops-backend:local .
	podman tag finops-backend:local $(IMAGE):latest

podman-push: podman-build
	podman push $(IMAGE):latest

run: build
	./bin/finops demo hello

clean:
	rm -rf bin dist

# Ad-hoc cross-compile examples:
# GOOS=linux GOARCH=amd64 go build -o bin/finops-linux-amd64 ./cli/cmd/finops
# GOOS=windows GOARCH=amd64 go build -o bin/finops.exe ./cli/cmd/finops
