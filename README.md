# skolar

Manage local [agent skills](https://agentskills.io) from git repos.
Reads and writes both [vercel-labs/skills](https://github.com/vercel-labs/skills) lockfiles and [`gh skill`](https://cli.github.com/manual/gh_skill) metadata.

![skolar reads and writes both the vercel-labs/skills lockfile and gh skill metadata](docs/flow.svg)

## Why

- Version-control skills from external repositories, say in your dotfiles
- Selectively install only the skills you need
- Review upstream diffs before applying changes

## Install

Install with [mise](https://mise.en.dev):

```sh
mise use -g packslip:github.com/h3y6e/skolar
```

Or with `go install`:

```sh
go install github.com/h3y6e/skolar@latest
```

Or download a binary from [Releases](https://github.com/h3y6e/skolar/releases).

## Usage

```sh
skolar add h3y6e/spec-skills                  # latest release, else default branch
skolar add h3y6e/spec-skills#main             # pin a ref
skolar add -s spec-plan h3y6e/spec-skills     # only some skills
skolar add -d dot_agents/exact_skills ...     # other destination (default .agents/skills)
skolar list
skolar check
skolar update                                 # show diffs, then confirm
skolar remove spec-plan
```

- `add`, `update`, and `remove` take `--dry-run`, and confirm before overwriting or deleting; pass `-y` without a TTY.
- `list --json` and `check --json` print `{"skills": [...]}` and `{"results": [...]}`, plus `"error": {"code", "message", "fix"}` on failure.
- Any flag except `--help` and `--version` can be set as `SKOLAR_<FLAG>`, e.g. `SKOLAR_DEST`.
- Exit status: 0 success, 1 failure (including a skill that could not be checked), 2 usage error or missing `-y`.

## Formats

A destination with a `skills-lock.json` beside it, or a non-GitHub source, uses the vercel-labs/skills lockfile. Otherwise skolar writes `gh skill` metadata into each `SKILL.md` and no lockfile, so skills installed by either tool are managed by both.

In `gh skill` format:

- Without `#ref`, `update` follows the highest non-prerelease semver tag.
- With `#ref`, the skill is pinned and `update` skips it; re-add to move the pin.
- Skills installed with `gh skill install --from-local` can be listed and removed, not updated.

## License

[MIT](LICENSE)
