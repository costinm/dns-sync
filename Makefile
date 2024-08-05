# Copyright 2017 The Kubernetes Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

.PHONY: cover cover-html
.DEFAULT_GOAL := build

# find or download controller-gen
# download controller-gen if necessary
controller-gen:
ifeq (, $(shell which controller-gen))
	@{ \
	set -e ;\
	go install sigs.k8s.io/controller-tools/cmd/controller-gen@v0.15.0 ;\
	}
CONTROLLER_GEN=$(GOBIN)/controller-gen
else
CONTROLLER_GEN=$(shell which controller-gen)
endif

# generates CRD using controller-gen
.PHONY: crd
crd: controller-gen
	${CONTROLLER_GEN} crd:crdVersions=v1 paths="./endpoint/..." paths="./pkg/apis/..." output:crd:stdout > manifests/crd.yaml

# The verify target runs tasks similar to the CI tasks, but without code coverage
.PHONY: test
test:
	go test -race -coverprofile=profile.cov ./...

# The build targets allow to build the binary and container image
.PHONY: build

BINARY        ?= dns-sync
SOURCES        = $(shell find . -name '*.go')
REGISTRY      ?= costinm
REPO_IMAGE         ?= $(REGISTRY)/$(BINARY)
VERSION       ?= $(shell git describe --tags --always --dirty --match "v*")
BUILD_FLAGS   ?= -v
LDFLAGS       ?= -X sigs.k8s.io/external-dns/pkg/apis/externaldns.Version=$(VERSION) -w -s
ARCH          ?= amd64
SHELL          = /bin/bash
IMG_PUSH      ?= true
IMG_SBOM      ?= none

build: build/$(BINARY)

build/$(BINARY): $(SOURCES)
	CGO_ENABLED=0 go build -o build/$(BINARY) $(BUILD_FLAGS) -ldflags "$(LDFLAGS)" .

push:
	@echo Context: ${BUILD_CONTEXT}
	@echo Image: ${IMAGE_REPO} ${IMAGE}  ${PUSH_IMAGE}
	@echo Tag: ${IMAGE_TAG}

	KO_DOCKER_REPO=${REPO_IMAGE} \
    VERSION=${VERSION} \
    ko build --tags ${IMAGE_TAG} --bare --sbom ${IMG_SBOM} \
      --image-label org.opencontainers.image.source="https://github.com/costinm/dns-sync" \
      --image-label org.opencontainers.image.revision=$(shell git rev-parse HEAD) \
      --push=${IMG_PUSH} .

clean:
	@rm -rf build
	@go clean -cache

ci:
	go get -v -t -d ./...
