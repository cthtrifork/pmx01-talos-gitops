# Rego ignore policy, applied by trivy to every finding (ignore-policy in the trivy config of helm-release.yaml).
# https://trivy.dev/docs/latest/configuration/filtering/#by-rego
# `input` is one vulnerability: input.VulnerabilityID, input.PkgName, input.InstalledVersion, input.Severity, ...
# https://www.openpolicyagent.org/projects/regal/rules/style
package trivy

import data.lib.trivy
import rego.v1

ignore if {
	# https://github.com/aquasecurity/trivy-db/blob/main/pkg/types/status.go#L20
	input.Status in {5, "will_not_fix"}
	not input.Severity in {"CRITICAL", "HIGH"}
}

ignore if {
	# https://github.com/aquasecurity/trivy-db/blob/main/pkg/types/status.go#L16
	input.Status in {1, "not_affected"}
}

# Ignore undisclosed CVEs and wait for disclosed CVE-XXX instead
ignore if {
	startswith(input.VulnerabilityID, "BDSA")
}

# Ignore a vulnerability which requires high privilege
ignore if {
	nvd_cvss_vector := trivy.parse_cvss_vector_v3(nvd_v3_vector)
	nvd_cvss_vector.PrivilegesRequired == "High"

	# Check against RedHat scores as well as NVD
	redhat_cvss_vector := trivy.parse_cvss_vector_v3(redhat_v3_vector)
	redhat_cvss_vector.PrivilegesRequired == "High"
}

# Ignore a vulnerability which requires user interaction
ignore if {
	nvd_cvss_vector := trivy.parse_cvss_vector_v3(nvd_v3_vector)
	nvd_cvss_vector.UserInteraction == "Required"

	# Check against RedHat scores as well as NVD
	redhat_cvss_vector := trivy.parse_cvss_vector_v3(redhat_v3_vector)
	redhat_cvss_vector.UserInteraction == "Required"
}

# Ignore a vulnerability which requires physical or local access to the host
ignore if {
	input.PkgName in { "openssl", "golang", "bash" }

	# Split CVSSv3 vector
	nvd_cvss_vector := trivy.parse_cvss_vector_v3(nvd_v3_vector)

	# Evaluate Attack Vector
	nvd_cvss_vector.AttackVector in {"Physical", "Local"}
}

# Ignore a vulnerability which requires physical access to the host
ignore if {
	# Split CVSSv3 vector
	nvd_cvss_vector := trivy.parse_cvss_vector_v3(nvd_v3_vector)

	# Evaluate Attack Vector
	nvd_cvss_vector.AttackVector in {"Physical"}
}

# Ignore vulnerabilities in OpenSSL if their severity is LOW, MEDIUM, or HIGH (but not CRITICAL)
# and they are not associated with CWE-119 or CWE-200.
ignore if {
	input.PkgName == "openssl"

	# Evaluate CWE-ID
	deny_cwe_ids := {
		"CWE-119", # Improper Restriction of Operations within the Bounds of a Memory Buffer
		"CWE-200", # Exposure of Sensitive Information to an Unauthorized Actor
	}

	count({x | x := input.CweIDs[_]; x == deny_cwe_ids[_]}) == 0
	input.Severity != "CRITICAL"
}

ignore if {
	input.Severity in {"LOW", "MEDIUM", "HIGH"}
	reason := "Low risk due to enforced 'proxy-only' egress at the network layer + least-privilege RBAC + NetworkPolicies"
	some cwe in input.CweIDs
	cwe in {"CWE-115", "CWE-295", "CWE-407"}
}

ignore if {
	input.Severity in {"LOW", "MEDIUM", "HIGH"}
	reason := "Uncontrolled Resource Consumption is at low risk due to enforced resource request and limits."
	some cwe in input.CweIDs
	cwe in {"CWE-400", "CWE-770"}
}

ignore if {
	input.Severity in {"LOW", "MEDIUM", "HIGH"}
	reason := "Cross-site Scripting only affects frontend components."
	some cwe in input.CweIDs
	cwe in {"CWE-79", "CWE-352", "CWE-601", "CWE-1021"}
}

# core linux, so highly unlikely to be exploitable in a containerized environment
ignore if {
	input.PkgName == "linux-libc-dev"
}

# Scoping: availability-only (CPU/memory exhaustion, crashes) is out of scope.
# CVEs with no CVSS v3 data are not ignored here.
ignore if {
	input.Severity != "CRITICAL"
	count(parsed) > 0
	not has_ci_impact
}

# Scoping: Local/Physical-only attack vectors are out of scope.
# ignore if {
# 	count(parsed) > 0
#	 not remotely_reachable
# }

# --- Helpers ---

nvd_v3_vector := input.CVSS.nvd.V3Vector

redhat_v3_vector := input.CVSS.redhat.V3Vector

# All CVSS v3 vectors, from any source
v3_vectors contains input.CVSS.nvd.V3Vector

v3_vectors contains input.CVSS.redhat.V3Vector

v3_vectors contains input.CVSS.ghsa.V3Vector

parsed contains p if {
	some v in v3_vectors
	p := trivy.parse_cvss_vector_v3(v)
}

has_ci_impact if {
	some p in parsed
	p.Confidentiality != "None"
}

has_ci_impact if {
	some p in parsed
	p.Integrity != "None"
}

remotely_reachable if {
	some p in parsed
	p.AttackVector in {"Network", "Adjacent"}
}