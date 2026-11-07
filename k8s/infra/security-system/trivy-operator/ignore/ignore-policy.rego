# Rego ignore policy, applied by trivy to every finding (ignore-policy in the trivy config of helm-release.yaml).
# https://trivy.dev/docs/latest/configuration/filtering/#by-rego
# `input` is one vulnerability: input.VulnerabilityID, input.PkgName, input.InstalledVersion, input.Severity, ...
package trivy

import rego.v1

default ignore := false

# Example: ignore a package everywhere.
# ignore if input.PkgName == "example-package"
