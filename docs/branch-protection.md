# Branch protection

`main` is protected by a repository ruleset so that nothing, including the Claude GitHub App
(`k8s/apps/claude-system`), can merge without a human review.

- `.github/CODEOWNERS` makes `@cthtrifork` the owner of every path.
- `.github/rulesets/main-protection.json` is the ruleset: changes to the default branch need a PR with one
  approval, that approval must come from a code owner, and it must be on the latest push. There are no bypass
  actors, and branch deletion and force pushes are blocked.

A ruleset is repository configuration, not a file GitHub reads from the tree, so it has to be imported once:
**Settings → Rules → Rulesets → New ruleset → Import a ruleset**, then pick the JSON file.

## Notes

- The App token has Pull requests read/write, which GitHub does not split into "open" and "merge". The ruleset is
  what stops the merge. Never add the App (or its installation) to the ruleset's bypass list or to the repo's
  admin/maintain roles.
- Renovate uses `"automergeType": "branch"` for patch and digest updates, which pushes straight to `main`. Under this
  ruleset that is rejected unless Renovate is allowed to bypass, or it is switched to `"automergeType": "pr"`
  (which still needs the code owner approval, so it becomes a manual merge).
