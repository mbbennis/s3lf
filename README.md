# s3lf

An lf-style terminal browser for Amazon S3. Miller columns, vim-ish keys,
read-only by default if you want it.

```
s3://my-bucket/logs/
  2024-08/      ▸ access.log               1.2MB
  2024-09/        error.log                  847B
  archive/        debug.log                  4.5KB
  index.json
                                                                    [default]
```

## Install

```
go install github.com/mbbennis/s3lf/cmd/s3lf@latest
```

## Run

Uses the standard AWS SDK credential chain — `~/.aws/credentials`,
`~/.aws/config`, SSO, env vars, IMDS.

```
s3lf
s3lf --profile prod
s3lf --read-only --region eu-west-1
```

| Flag | Default | Purpose |
| --- | --- | --- |
| `--profile` | env / `default` | AWS profile to start with |
| `--region` | profile's | override region |
| `--read-only` | off | disable delete + edit-save |
| `--download-dir` | cwd | where `y` saves files |
| `--edit-size-limit` | 10 MiB | refuse `e`/`v` above this |

## Keys

Press `?` at any time for the in-app reference.

| | |
| --- | --- |
| `j`/`k` | move down/up |
| `l`/`Enter`/`→` | enter directory |
| `h`/`Backspace`/`←` | go back |
| `gg`/`G` | top / bottom of loaded |
| `R` | refresh listing |
| `/` then `n`/`N` | search (smartcase) |
| `y` | download |
| `v` | view in `$PAGER` |
| `e` | edit in `$EDITOR` |
| `o` | open with system default |
| `D` | delete (type filename to confirm) |
| `P` | switch AWS profile |
| `?` | toggle help |
| `q` / `Ctrl-C` | quit |

`$EDITOR` and `$PAGER` are required for `e` and `v`. No defaults — the
right fallback depends on the user.

## Design notes

- **One `ListObjectsV2(delimiter="/")` per pane**, behind an LRU + singleflight
  cache (TTL 60s). Navigating back to a recent pane is instant.
- **Pagination** is lazy: the first 1000 entries are fetched eagerly; more
  pages are pulled when the cursor gets within 50 rows of the end.
- **Conditional GET on edit**: the download uses `If-Match` against the
  ETag from the prior `HEAD`. A 412 means the object changed; the TUI
  asks you to retry rather than handing the editor stale bytes paired
  with an out-of-date ETag.
- **Conditional `PutObject` on save**: same ETag is reused as `If-Match`.
  Conflict preserves the local temp file and reports its path.
- **Per-profile cache**: switching profiles via `P` rebuilds the cache
  and S3 client. No cross-account listing leakage.

## Build & test

```
go build ./...
go test ./...
go test -race ./...
```
