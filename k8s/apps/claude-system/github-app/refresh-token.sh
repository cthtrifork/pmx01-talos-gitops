#!/bin/sh
# Exchange the App JWT for a one-hour installation token, narrowed to one repository and the
# permissions the runner needs, and store it in the Secret the runner mounts.
set -eu

installation_id=$(tr -d '\n' </app/installation-id)

# GITHUB_REPOSITORIES is a comma-separated list of repository names
body=$(jq -nc --arg repos "$GITHUB_REPOSITORIES" \
    '{repositories: ($repos | split(",")), permissions: {contents: "write", pull_requests: "write", workflows: "write"}}')

curl -fsS -X POST \
    -H "Authorization: Bearer $(cat /work/jwt)" \
    -H "Accept: application/vnd.github+json" \
    -H "X-GitHub-Api-Version: 2022-11-28" \
    -d "$body" \
    "https://api.github.com/app/installations/${installation_id}/access_tokens" |
    jq -er .token >/work/token

kubectl create secret generic claude-github-token -n claude-system \
    --from-file=token=/work/token --dry-run=client -o yaml | kubectl apply -f -
echo "claude-github-token refreshed"
