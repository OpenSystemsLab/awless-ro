# awless-ro v0.6.2

S3 object sync no longer keeps every object in memory.

This is the OpenSystemsLab build of awless-ro. It is what the awless MCP server runs.

### Changed

- With `aws.storage.s3object.sync` on, a sync keeps the number and total size of the
  objects in each bucket instead of every object. It used to hold them all until the
  end of the sync, plus a copy of the bucket per object, so memory grew with the
  number of objects in the account, and a few busy buckets took a 512 MiB container
  to its limit in seconds. A storage sync over 6 buckets and about 630,000 objects
  now peaks under 60 MB. API calls are unchanged: one paginated listing per bucket.
- `inspect -i bucket_sizer` reads those totals, and says how many buckets have no
  size when a listing failed, so the total is not read as the account's.
- The sync line counts objects from the totals.

### Added

- `aws.storage.s3object.keep` (off by default) keeps every object in the local graph
  as before, for `list s3objects --local`. Without it, `list s3objects --local` says
  why it is empty. `list s3objects` without `--local` asks AWS either way.

If your local copy was synced by an earlier version with object sync on, run
`awless-ro sync --storage` once so the buckets get their totals.

### Install

Download the archive for your platform below and check it:

```sh
shasum -a 256 -c SHA256SUMS --ignore-missing
```

Not on Homebrew: `brew install theazz/tap/awless-ro` installs the upstream release,
which does not have JSON output yet.
