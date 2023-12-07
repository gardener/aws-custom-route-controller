# SPDX-FileCopyrightText: 2021 SAP SE or an SAP affiliate company and Gardener contributors
#
# SPDX-License-Identifier: Apache-2.0

REGISTRY              := europe-docker.pkg.dev/gardener-project/public
EXECUTABLE            := aws-custom-route-controller
PROJECT               := github.com/gardener/aws-custom-route-controller
IMAGE_REPOSITORY      := $(REGISTRY)/gardener/aws-custom-route-controller
REPO_ROOT             := $(shell dirname $(realpath $(lastword $(MAKEFILE_LIST))))
VERSION               := $(shell cat VERSION)
IMAGE_TAG             := $(VERSION)
EFFECTIVE_VERSION     := $(VERSION)-$(shell git rev-parse HEAD)
GOARCH                := amd64

TOOLS_DIR                  := $(REPO_ROOT)/hack
TOOLS_BIN_DIR              := $(TOOLS_DIR)/bin
MOCKGEN                    := $(TOOLS_BIN_DIR)/mockgen

.PHONY: revendor
revendor:
	@env GO111MODULE=on go mod vendor
	@env GO111MODULE=on go mod tidy

# build local executable
.PHONY: build-local
build-local:
	@CGO_ENABLED=1 GO111MODULE=on go build -o $(EXECUTABLE) \
		-race \
		-mod=vendor \
		-ldflags "-X 'main.Version=$(EFFECTIVE_VERSION)' -X 'main.ImageTag=$(IMAGE_TAG)'"\
		main.go

.PHONY: release
release:
	@CGO_ENABLED=0 GOOS=linux GOARCH=$(GOARCH) GO111MODULE=on go build -o $(EXECUTABLE) \
        -mod=vendor \
        -ldflags "-w -X 'main.Version=$(EFFECTIVE_VERSION)' -X 'main.ImageTag=$(IMAGE_TAG)'"\
		main.go

.PHONY: check
check: $(GOIMPORTS)
	go vet ./...

# Run go fmt against code
.PHONY: format
format:
	@env GO111MODULE=on go fmt ./...

.PHONY: docker-images
docker-images:
	@docker build -t $(IMAGE_REPOSITORY):$(IMAGE_TAG) -f Dockerfile .

.PHONY: docker-images-linux-amd64
docker-images-linux-amd64:
	@docker buildx build --platform linux/amd64 -t $(IMAGE_REPOSITORY):$(IMAGE_TAG) -f Dockerfile .

$(GOIMPORTS): go.mod
	go build -o $(GOIMPORTS) golang.org/x/tools/cmd/goimports

$(MOCKGEN): go.mod
	go build -o $(MOCKGEN) github.com/golang/mock/mockgen

.PHONY: generate
generate: $(MOCKGEN)
	@go generate ./pkg/...

# Run tests
.PHONY: test
test:
	@env GO111MODULE=on go test ./pkg/...

.PHONY: update-dependencies
update-dependencies:
	@env GO111MODULE=on go get -u
	@make revendor

.PHONY: verify
verify: check format test
