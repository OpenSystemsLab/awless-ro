# awless-ro v0.6.1

Fixes for answers that looked complete and were not, found reading the synced copy
through `--format json`.

This is the OpenSystemsLab build of awless-ro. It is what the awless MCP server runs.

### Fixed

- `list --local` says when the service it reads was never synced in that region, also
  when another region of the profile was. A sync of us-west-2 left the global files
  behind, and `list instances --local -r eu-west-1` answered an empty list.
- With `--format json`, that hint is a plain line on stderr, like the inspectors'
  notes, instead of an `[info]` log line.
- `inspect -i open_buckets` says that it reads bucket ACLs only, not bucket policies
  or Block Public Access, so an empty answer no longer reads as "no public buckets".

### Install

Download the archive for your platform below and check it:

```sh
shasum -a 256 -c SHA256SUMS --ignore-missing
```

Not on Homebrew: `brew install theazz/tap/awless-ro` installs the upstream release,
which does not have JSON output yet.
