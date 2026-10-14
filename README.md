# skills

A CLI tool to manage local [agent skills](https://agentskills.io), inspired by [vercel-labs/skills](https://github.com/vercel-labs/skills).

Compatible with both [vercel-labs/skills](https://github.com/vercel-labs/skills) (`skills-lock.json`) and [`gh skill`](https://cli.github.com/manual/gh_skill) (SKILL.md frontmatter metadata).

## Why

- Version-control skills from external repositories, say in your dotfiles
- Selectively install only the skills you need
- Review upstream diffs before applying changes

## Install

```sh
go install github.com/h3y6e/skills@latest
```

Or download a binary from [Releases](https://github.com/h3y6e/skills/releases).

## Usage

```sh
skills add h3y6e/spec-skills                               # Install skills from a repository
skills add h3y6e/spec-skills#main                          # Install from an explicit ref
skills add --list h3y6e/spec-skills                        # Preview available skills
skills add -s spec-plan h3y6e/spec-skills                  # Install specific skills only
skills add -d dot_agents/exact_skills h3y6e/spec-skills    # Custom destination (e.g. chezmoi)

skills list                                                # Show installed skills
skills check                                               # Check for upstream updates
skills update                                              # Review diffs and apply updates
skills update h3y6e/spec-skills#main                      # Update only entries installed from that ref
skills update -y                                           # Apply without confirmation (CI-safe)
skills remove spec-plan                                    # Remove a skill
```

### Install formats

Skills are installed to `.agents/skills/` by default (configurable with `-d`). `skills` manages two install formats, chosen per destination:

- **gh skill format**: used for GitHub sources when no `skills-lock.json` exists alongside the destination. Like `gh skill install`, source tracking metadata (`github-repo`, `github-ref`, `github-tree-sha`, `github-path`) is injected into each `SKILL.md` and no lockfile is written. Skills installed by `gh skill` are likewise recognized by `skills list`, `check`, `update`, and `remove`. Skills nested one level under a namespace (`skills/<namespace>/<name>`) are discovered and installed flat under their leaf name, as `gh skill` does.
- **Lockfile format**: used when a `skills-lock.json` already exists, and always for non-GitHub sources (GitLab, generic git, local paths), which gh skill metadata cannot track. The format is compatible with [vercel-labs/skills](https://github.com/vercel-labs/skills).

In gh skill format, versioning follows `gh skill` semantics:

- Without `#ref`, the latest release tag is installed (falling back to the default branch), and `update` re-resolves the latest release. Since `skills` is git-only, the "latest release" is approximated by the highest non-prerelease semver tag.
- With `source#ref`, the skill is pinned (`github-pinned`) and `update` skips it. Re-add with a new ref to move the pin.
- Skills recorded with a local path (`local-path` metadata, e.g. from `gh skill install --from-local`) can be listed and removed, but not updated.

### Directory structure

```
.agents/skills/
├── spec-plan/
│   └── SKILL.md       # gh skill format: metadata injected into frontmatter
└── spec-specify/
    └── SKILL.md
skills-lock.json       # lockfile format only
```

## License

[MIT](LICENSE)
