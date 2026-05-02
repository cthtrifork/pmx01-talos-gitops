#!/usr/bin/env bash
set -euo pipefail

# Get the directory of the current script
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Enable Helm OCI support
export HELM_EXPERIMENTAL_OCI=1

echo "Rendering manifests for directory: $(pwd)"

# Render manifests from upstream without helm chart
# Kustomize build file called render-kustomization.yaml if it exists
rendered_files=0
while IFS= read -r -d '' file; do
  rm -rf ./rendered
  echo "Found kustomization file for rendering: $file"
  mkdir -p .tmp
  mkdir -p rendered
  cp ./*.yaml .tmp/
  mv ./.tmp/render-kustomization.yaml .tmp/kustomization.yaml
  output_file="rendered/$(basename "$PWD").yaml"
  kustomize build ".tmp/" -o "$output_file"
  rm -r ".tmp"
  rendered_files=$((rendered_files + 1))

done < <(find . -type f -path "./render-kustomization.yaml" -print0)

if [[ $rendered_files -gt 0 ]]; then
  touch ./rendered/.dummy
  echo "Rendered $rendered_files kustomization file(s). Exiting."
  exit 0
fi

# Define the Helm release and repository files
HELM_REPOSITORY_FILE="./helm-repository.yaml"
HELM_RELEASE_FILE="./helm-release.yaml"
HELM_VALUES_FILE="./helm-values.yaml"

# Function to extract and validate values
extract_and_validate() {
  local field="$1"
  local file="$2"
  # fallback to true
  local validate="${3:-true}"
  local value
  value=$(yq eval "$field" "$file" | head -n 1) # get first result if it is a multi-document file
  if [[ $validate == "true" && ("$value" == "null" || -z "$value") ]]; then
    echo "Error: Missing or invalid value for $field in '$file'." >&2
    exit 1
  fi
  echo "$value"
}

# Extract and validate all required values
flux envsubst <"$HELM_REPOSITORY_FILE" >"$HELM_REPOSITORY_FILE.tmp"
HELM_REPOSITORY_NAME=$(extract_and_validate '.metadata.name' "$HELM_REPOSITORY_FILE".tmp)

HELM_REPOSITORY_URL=$(extract_and_validate '.metadata.annotations.["kube-kraken.dk/chart-mirror-source"]' "$HELM_REPOSITORY_FILE".tmp "false")

flux envsubst <"$HELM_REPOSITORY_FILE" >"$HELM_REPOSITORY_FILE.tmp"
if [[ "$HELM_REPOSITORY_URL" == "null" ]]; then
  HELM_REPOSITORY_URL=$(extract_and_validate '.spec.url' "$HELM_REPOSITORY_FILE".tmp)
fi

HELM_REPOSITORY_TYPE=$(extract_and_validate '.kind' "$HELM_REPOSITORY_FILE".tmp "false")

HELM_RELEASE_NAME=$(extract_and_validate '.metadata.name' "$HELM_RELEASE_FILE")
HELM_RELEASE_NAMESPACE=$(extract_and_validate '.metadata.namespace' "$HELM_RELEASE_FILE")

echo "Extracted Helm release name: $HELM_RELEASE_NAME"
echo "Extracted Helm release namespace: $HELM_RELEASE_NAMESPACE"

# Extract targetNamespace if set
TARGET_NAMESPACE=$(yq eval -r 'select(di == 1) | .spec.targetNamespace' "$HELM_RELEASE_FILE")
if [[ "$TARGET_NAMESPACE" != "null" && -n "$TARGET_NAMESPACE" ]]; then
  HELM_RELEASE_NAMESPACE="$TARGET_NAMESPACE"
  echo "Using targetNamespace from Helm release: $HELM_RELEASE_NAMESPACE"
fi

if [[ "$HELM_REPOSITORY_TYPE" == "OCIRepository" ]]; then
  HELM_RELEASE_CHART=$(extract_and_validate '.spec.chartRef.name' "$HELM_RELEASE_FILE")
  HELM_RELEASE_CHART_VERSION=$(extract_and_validate '.spec.ref.tag' "$HELM_REPOSITORY_FILE".tmp)
else
  HELM_RELEASE_CHART=$(extract_and_validate '.spec.chart.spec.chart' "$HELM_RELEASE_FILE")
  HELM_RELEASE_CHART_VERSION=$(extract_and_validate '.spec.chart.spec.version' "$HELM_RELEASE_FILE")
fi

if [[ -f $HELM_REPOSITORY_FILE.tmp ]]; then
  rm $HELM_REPOSITORY_FILE.tmp
fi
rm -rf ./rendered

prepare_values() {
  local tmp_values_file="$1"

  if [[ -f ./.render-env ]]; then
    while read -r line; do
      export $line
    done <./.render-env
  fi

  if [[ ! -f "$HELM_VALUES_FILE" ]]; then
    flux envsubst < <(yq eval '.spec.values' "$HELM_RELEASE_FILE") >"$tmp_values_file"
  else
    flux envsubst <"$HELM_VALUES_FILE" >"$tmp_values_file"
  fi

  if [[ -f ./.render-env ]]; then
    while read -r line; do
      # Extract variable name before '='
      var_name=$(echo "$line" | cut -d '=' -f 1)
      unset $var_name
    done <./.render-env
  fi
}

# Ensure namespace is set in all rendered manifests
post_build() {
  echo "Setting namespace for rendered manifests: $HELM_RELEASE_NAMESPACE"
  if [[ ! -d "./rendered" ]]; then
    echo "No rendered directory found. Skipping..." >&2
    exit 1
  fi
  find ./rendered -type f -name "*.yaml" | while read -r file; do
    HELM_RELEASE_NAMESPACE=$HELM_RELEASE_NAMESPACE yq '. |= (
        with(select(.metadata != null and .metadata.namespace == null);
          .metadata.namespace = env(HELM_RELEASE_NAMESPACE)
        )
      )' -i "$file"
  done

  # Split List kind documents into separate documents and ensure document starts with ---
  find ./rendered -type f -name "*.yaml" | while read -r file; do
    if yq eval 'select(.kind == "List") | .kind' "$file" | grep -q "List"; then
      yq eval '.items[] | split_doc' "$file" >"$file.tmp" && mv "$file.tmp" "$file"
    fi
  done
}

apply_helm_release_post_renderer() {
  # Extract post-render patch configuration
  PATCHES=$(yq '.spec.postRenderers[].kustomize' $HELM_RELEASE_FILE)

  find rendered/ ! -name 'helm-default-values.yaml' -name '*.yaml' | sed 's|^rendered/||' | while IFS= read -r file; do
    # Build kustomization overlay from the extracted patch
    cat >"rendered/kustomization.yaml" <<EOF
  apiVersion: kustomize.config.k8s.io/v1beta1
  kind: Kustomization
  sortOptions:
    order: fifo
  resources:
  - $file
  $PATCHES
EOF
    # Apply the kustomize overlay to the rendered manifests
    kustomize build "./rendered" -o "rendered/$file"

    rm rendered/kustomization.yaml
  done

}

# change tracking
mkdir -p ./rendered
touch ./rendered/.dummy

# Handle repository type
if [[ "$HELM_REPOSITORY_TYPE" == "OCIRepository" ]]; then
  echo "Detected OCI Helm repository."

  # Temporary values file name
  TMP_VALUES_FILE="./helm-release-values.tmp"

  # Prepare values file from helm-release.yaml
  prepare_values "$TMP_VALUES_FILE"

  HELM_RELEASE_CHART_VERSION_TAG=$(echo "$HELM_RELEASE_CHART_VERSION" | cut -d '@' -f1)

  # Template the Helm chart
  echo "Templating Helm chart in $HELM_RELEASE_NAMESPACE to ./rendered"
  helm template "$HELM_RELEASE_NAME" "${HELM_REPOSITORY_URL}:${HELM_RELEASE_CHART_VERSION}" \
    --namespace "$HELM_RELEASE_NAMESPACE" \
    --version "$HELM_RELEASE_CHART_VERSION_TAG" \
    --values "$TMP_VALUES_FILE" \
    --output-dir "./rendered" \
    --skip-tests

  if [[ -f $TMP_VALUES_FILE ]]; then
    rm -f "$TMP_VALUES_FILE"
  fi

  post_build

  helm show values --version "${HELM_RELEASE_CHART_VERSION_TAG}" "${HELM_REPOSITORY_URL}:${HELM_RELEASE_CHART_VERSION}" | tee -a ./rendered/helm-default-values.yaml >/dev/null
else
  echo "Detected HTTP Helm repository."

  # Add Helm repository
  echo "Adding Helm repository '$HELM_REPOSITORY_NAME' from URL '$HELM_REPOSITORY_URL'"
  helm repo add "$HELM_REPOSITORY_NAME" "$HELM_REPOSITORY_URL" --force-update
  helm repo update "$HELM_REPOSITORY_NAME"

  # Template the Helm chart
  echo "Templating Helm chart to ./rendered"

  # Temporary values file name
  TMP_VALUES_FILE="./helm-release-values.tmp"

  # Prepare values file from helm-release.yaml
  prepare_values "$TMP_VALUES_FILE"

  helm template "$HELM_RELEASE_NAME" "$HELM_REPOSITORY_NAME/$HELM_RELEASE_CHART" \
    --version "$HELM_RELEASE_CHART_VERSION" \
    --namespace "$HELM_RELEASE_NAMESPACE" \
    --values "$TMP_VALUES_FILE" \
    --output-dir "./rendered" \
    --skip-tests

  if [[ -f $TMP_VALUES_FILE ]]; then
    rm -f "$TMP_VALUES_FILE"
  fi

  post_build

  helm show values --version "$HELM_RELEASE_CHART_VERSION" "$HELM_REPOSITORY_NAME/$HELM_RELEASE_CHART" | tee -a ./rendered/helm-default-values.yaml >/dev/null
fi

TEMPLATES_DIR="./rendered/$HELM_RELEASE_NAME/templates"
if ! [[ -d "$TEMPLATES_DIR" ]]; then
  echo "Templates directory $TEMPLATES_DIR not found / generated. Source dir: $(pwd)"
else
  # Move rendered templates to root directory
  if [[ $(find ./rendered/"$HELM_RELEASE_NAME" -mindepth 1 -maxdepth 1 -type d | wc -l) -gt 1 ]]; then
    echo "Multiple directories found in $TEMPLATES_DIR."
    mv "$TEMPLATES_DIR"/* ./rendered/"$HELM_RELEASE_NAME"
    rm -r "$TEMPLATES_DIR"
  else
    echo "Only one directory found in $TEMPLATES_DIR. Moving all files to rendered directory."
    mv "$TEMPLATES_DIR"/* ./rendered
    rm -r "./rendered/$HELM_RELEASE_NAME"
  fi
fi

# Ensure proper formatting
yq -i "./rendered/helm-default-values.yaml"

# Apply post-render patches if it exists in the Helm release
POST_RENDERERS=$(yq eval '.spec.postRenderers' "$HELM_RELEASE_FILE")
if [[ "$POST_RENDERERS" == "null" || -z "$POST_RENDERERS" ]]; then
  echo "No post-render patches found in Helm release. Skipping..."
else
  echo "Applying post-render patches from Helm release."
  apply_helm_release_post_renderer
fi

# Discard changes to resources with the label: cilium.io/helm-template-non-idempotent: "true" since they are expected to be non-idempotent and may cause issues in CI if they change on every render
find ./rendered -type f -name "*.yaml" -print0 |
while IFS= read -r -d '' file; do
  if yq eval -e '.metadata.labels["cilium.io/helm-template-non-idempotent"] == "true"' "$file" >/dev/null 2>&1; then
    if git ls-files --error-unmatch "$file" >/dev/null 2>&1; then
      echo "Reverting tracked non-idempotent resource: $file"
      git checkout -- "$file"
    else
      echo "Leaving new file untouched: $file"
    fi
  fi
done

echo "Manifests rendered successfully in: ./rendered"
