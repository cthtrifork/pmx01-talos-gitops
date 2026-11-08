
# Define shell
SHELL = /usr/bin/env bash

# Define phony targets to avoid file conflicts
.PHONY: render-manifests-incremental render-manifests

PROJECT_ROOT := $(shell git rev-parse --show-toplevel)
export CONTEXT=$(BUILD_CONTEXT)

# Directories that contain both helm-release.yaml and helm-repository.yaml
HELM_DIRS := $(shell \
  find ./k8s -type f -name helm-release.yaml -print0 \
  | xargs -0 -n1 dirname \
  | while read d; do \
      [ -f "$$d/helm-repository.yaml" ] && echo "$$d"; \
    done \
  | sort -u \
)


RENDER_STAMPS := $(addsuffix /rendered/.dummy,$(HELM_DIRS))

# render manifest updates .dummy
# if any %/helm-release.yaml %/helm-repository.yaml %/helm-values.yaml is NEWER than your local .dummy
# an incremental render will be performed
render-manifests-incremental: $(RENDER_STAMPS)

%/rendered/.dummy: %/helm-release.yaml %/helm-repository.yaml %/helm-values.yaml
	@mkdir -p $(@D)
	cd $(@D)/.. && $(PROJECT_ROOT)/scripts/ci/render-manifests.sh
	@touch $@

%/rendered/.dummy: %/helm-release.yaml %/helm-repository.yaml
	@mkdir -p $(@D)
	cd $(@D)/.. && $(PROJECT_ROOT)/scripts/ci/render-manifests.sh
	@touch $@

.SECONDEXPANSION:
%/rendered/.dummy: %/render-kustomization.yaml $$(@D)/../*.yaml
	@mkdir -p $(@D)
	cd $(@D)/.. && $(PROJECT_ROOT)/scripts/ci/render-manifests.sh
	@touch $@

# Fallback: always rerun renders, regardless of timestamps
# Renders every component by default; override RENDER_DIRS="dir1 dir2" to limit it
ifeq ($(strip $(RENDER_DIRS)),)
override RENDER_DIRS := $(HELM_DIRS)
endif
render-manifests:
	@set -e; \
	for d in $(RENDER_DIRS); do \
	  mkdir -p "$$d/rendered"; \
	  (cd "$$d" && $(PROJECT_ROOT)/scripts/ci/render-manifests.sh); \
	done
