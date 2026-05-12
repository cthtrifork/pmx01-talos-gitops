ZOT_VERSION ?= 2.1.14
EXT_VERSION ?= $(ZOT_VERSION)
REGISTRY ?= ghcr.io/casperthygesen
IMAGE ?= $(REGISTRY)/talos-zot-extension
PLATFORMS ?= linux/amd64,linux/arm64

.PHONY: build push buildx inspect

build:
	docker build \
		--build-arg ZOT_VERSION=$(ZOT_VERSION) \
		-t $(IMAGE):$(EXT_VERSION) .

push:
	docker buildx build \
		--platform $(PLATFORMS) \
		--build-arg ZOT_VERSION=$(ZOT_VERSION) \
		-t $(IMAGE):$(EXT_VERSION) \
		--push .

inspect:
	crane manifest $(IMAGE):$(EXT_VERSION)