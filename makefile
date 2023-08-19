SHELL := /bin/bash

.PHONY: help
help:
	@echo "Usage make <TARGET>"
	@echo ""
	@echo "  Targets:"
	@echo "	   build				Build the todo executable"
	@echo "	   build-cgo			Build the todo executable with CGO_ENABLED=0"
	@echo "	   docker				Build docker container"


.PHONY: build
build:
	go build .

.PHONY: build-cgo
build-cgo:
	CGO_ENABLED=0 go build .

.PHONY: docker
docker:
	docker build --tag architectingsoftware/containerd-events:v1  -f ./dockerfile .

