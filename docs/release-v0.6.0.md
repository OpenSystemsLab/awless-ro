# awless-ro v0.6.0

JSON from every command: `--format json` now works on `show`, `whoami`, `inspect`,
`tail`, `config`, `switch`, `sync` and `version`, not only `list`. Nothing but JSON
goes to stdout, so the output can be piped straight into a program.

This is the OpenSystemsLab build of awless-ro. It is what the awless MCP server runs.

### Added

- `--format json` on every command but `ssh`. `--format` is now a global flag;
  `list --format` is unchanged. `tail` prints one JSON object per line, so `--follow`
  streams. See [JSON output](https://github.com/OpenSystemsLab/awless-ro#json-output).
- `list --ids --format json` prints an array of ids.

### Changed

- A command asked for a `--format` it cannot print (`whoami --format csv`) exits 1
  before doing anything. `list` keeps all five formats.
- Firewall rules, routes, grants and the other structured lists print in a fixed
  order, in every format.
- The MFA code prompt, the first-run region prompt and `config set`'s value prompt
  are written to stderr.

### Fixed

- `sync` says which services it could not read, and exits 1 when nothing could be
  synced, instead of reporting an empty account.
- `show` exits 1 with "not found" when the resource is gone after the refresh.
- `list --format json` with no results prints `[]` instead of `null`.

### Install

Download the archive for your platform below and check it:

```sh
shasum -a 256 -c SHA256SUMS --ignore-missing
```

Not on Homebrew: `brew install theazz/tap/awless-ro` installs the upstream release,
which does not have JSON output yet.
