# Rego ignore policy, applied by trivy to every finding: the trivy-operator scans (ignore-policy in the trivy
# config of helm-release.yaml) and the image scan on PRs (.github/workflows/scan-images.yaml).
# https://trivy.dev/docs/latest/configuration/filtering/#by-rego
# `input` is one vulnerability: input.VulnerabilityID, input.PkgName, input.InstalledVersion, input.Severity, ...
# Style: https://www.openpolicyagent.org/projects/regal/rules/style
package trivy

import rego.v1

import data.lib.trivy

default ignore := false

# --- Vendor status ---

# https://github.com/aquasecurity/trivy-db/blob/main/pkg/types/status.go#L20
ignore if {
	input.Status in {5, "will_not_fix"}
	not input.Severity in {"CRITICAL", "HIGH"}
}

# https://github.com/aquasecurity/trivy-db/blob/main/pkg/types/status.go#L16
ignore if input.Status in {1, "not_affected"}

# Undisclosed CVEs: wait for the disclosed CVE-XXX instead.
ignore if startswith(input.VulnerabilityID, "BDSA")

# --- Attack vector and preconditions (CVSS v3) ---

# Requires high privileges, according to both NVD and RedHat.
ignore if {
	nvd := trivy.parse_cvss_vector_v3(nvd_v3_vector)
	redhat := trivy.parse_cvss_vector_v3(redhat_v3_vector)
	nvd.PrivilegesRequired == "High"
	redhat.PrivilegesRequired == "High"
}

# Requires user interaction, according to both NVD and RedHat.
ignore if {
	nvd := trivy.parse_cvss_vector_v3(nvd_v3_vector)
	redhat := trivy.parse_cvss_vector_v3(redhat_v3_vector)
	nvd.UserInteraction == "Required"
	redhat.UserInteraction == "Required"
}

# Requires local or physical access to the host, in packages where that is how they are used.
ignore if {
	input.PkgName in {"openssl", "golang", "bash"}
	nvd := trivy.parse_cvss_vector_v3(nvd_v3_vector)
	nvd.AttackVector in {"Physical", "Local"}
}

# Requires physical access to the host.
ignore if {
	nvd := trivy.parse_cvss_vector_v3(nvd_v3_vector)
	nvd.AttackVector == "Physical"
}

# High attack complexity and not remotely exploitable, below CRITICAL.
ignore if {
	input.Severity != "CRITICAL"
	nvd := trivy.parse_cvss_vector_v3(nvd_v3_vector)
	nvd.AttackComplexity == "High"
	nvd.AttackVector != "Network"
}

# --- Packages ---

# Go standard library issues that only affect Windows; the cluster runs Linux.
ignore if {
	input.PkgName == "stdlib"
	input.Severity != "CRITICAL"
	contains(input.Description, "on Windows")
}

# Language packages under test, example or testdata directories are not run in production.
# PkgPath is only set for language packages, not for OS packages.
ignore if {
	input.Severity != "CRITICAL"
	regex.match(`(^|/)(testdata|tests?|examples?)/`, input.PkgPath)
}

# OpenSSL, except critical ones and those of memory-safety (CWE-119) or information-disclosure (CWE-200) kind.
openssl_kept_cwes := {
	"CWE-119", # Improper Restriction of Operations within the Bounds of a Memory Buffer
	"CWE-200", # Exposure of Sensitive Information to an Unauthorized Actor
}

ignore if {
	input.PkgName == "openssl"
	input.Severity != "CRITICAL"
	count(openssl_kept_cwes & {cwe | some cwe in input.CweIDs}) == 0
}

# Core linux, so highly unlikely to be exploitable in a containerized environment.
ignore if input.PkgName == "linux-libc-dev"

# --- Weaknesses mitigated by the cluster setup (never ignores CRITICAL) ---

# Low risk due to enforced "proxy-only" egress at the network layer + least-privilege RBAC + NetworkPolicies.
# CWE-115 misinterpretation of input, CWE-295 improper certificate validation, CWE-407 algorithmic complexity.
ignore if mitigated_cwe({"CWE-115", "CWE-295", "CWE-407"})

# Uncontrolled resource consumption is low risk due to enforced resource requests and limits.
# CWE-400/770 resource consumption, CWE-674 recursion, CWE-835 infinite loop, CWE-1333 regex DoS,
# CWE-789 huge allocation, CWE-476 null pointer crash.
ignore if mitigated_cwe({"CWE-400", "CWE-770", "CWE-674", "CWE-835", "CWE-1333", "CWE-789", "CWE-476"})

# Cross-site scripting only affects frontend components.
# CWE-79 XSS, CWE-352 CSRF, CWE-601 open redirect, CWE-1021 clickjacking.
ignore if mitigated_cwe({"CWE-79", "CWE-352", "CWE-601", "CWE-1021"})

mitigated_cwe(cwes) if {
	input.Severity in {"LOW", "MEDIUM", "HIGH"}
	some cwe in input.CweIDs
	cwe in cwes
}

# --- Impact scoping ---

# Availability-only impact (CPU/memory exhaustion, crashes) is out of scope.
# CVEs without CVSS v3 data are not ignored here.
ignore if {
	input.Severity != "CRITICAL"
	count(parsed) > 0
	not has_ci_impact
}

# --- Helpers ---

nvd_v3_vector := input.CVSS.nvd.V3Vector

redhat_v3_vector := input.CVSS.redhat.V3Vector

# Parsed CVSS v3 vectors, from any source.
parsed contains p if {
	some source in {"nvd", "redhat", "ghsa"}
	p := trivy.parse_cvss_vector_v3(input.CVSS[source].V3Vector)
}

# Confidentiality or integrity is affected, according to at least one source.
has_ci_impact if {
	some p in parsed
	p.Confidentiality != "None"
}

has_ci_impact if {
	some p in parsed
	p.Integrity != "None"
}
