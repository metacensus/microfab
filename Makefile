#
# SPDX-License-Identifier: Apache-2.0
#

include fabric.env

.PHONY: all lint unit integration

all: lint unit

generate:
	go generate ./...

lint:
	./scripts/lint.sh

unit:
	go run github.com/onsi/ginkgo/ginkgo -skipPackage integration ./...

integration:
	go run github.com/onsi/ginkgo/ginkgo integration

binary:
	go build -o microfabd cmd/microfabd/main.go	
	go build -o microfab cmd/microfab/main.go

.PHONY: docker
docker:
	docker build -t microfab -f Dockerfile2 --build-arg FABRIC_VERSION=$(FABRIC_VERSION) --build-arg FABRIC_CA_VERSION=$(FABRIC_CA_VERSION) .
