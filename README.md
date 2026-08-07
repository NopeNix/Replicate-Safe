# Replicate-Safe

Don't lose what you paid for. A tiny Docker container that, given a Replicate
API token, continuously downloads every prediction (and its output files)
created under that account to a folder on your machine.

Lightweight: single static Go binary on `distroless/static` (~10–15 MB image).

## Quick start

### 1. Get a Replicate API token

Create one at <https://replicate.com/account/api-tokens>. The token must belong
to the user or organization whose predictions you want to back up.

### 2. Configure

```bash
cp .env.example .env
# edit .env and paste your token
```

### 3. Run with docker compose

```bash
chmod 777 ./data        # distroless container runs as uid 65532 (nonroot)
docker compose up -d
docker compose logs -f replicate-safe
```

Files will appear in `./data/`. The state file `./data/.state.json` tracks
which predictions have already been processed.

> **Why `chmod 777`?** The base image `gcr.io/distroless/static:nonroot` runs
> as user `nonroot` (uid 65532). Docker's bind mount preserves the host
> directory's ownership, so the container needs write permission. `chmod 777`
> is fine for a single-tenant backup volume; alternatives are `chown 65532
> ./data` or pre-creating files with the right uid.

### 4. Or run with plain docker

```bash
mkdir -p ./data
chmod 777 ./data         # see note above about nonroot
docker run -d --name replicate-safe --restart unless-stopped \
  -e REPLICATE_API_TOKEN=r8_xxxxxxxx \
  -e POLL_INTERVAL=900 \
  -v "$(pwd)/data:/data" \
  replicate-safe:local
```

(If you used `docker compose build` the image is `replicate-safe:local`;
otherwise `docker build -t replicate-safe:local .` first.)

## Configuration

All settings come from environment variables:

| Variable | Default | Notes |
|---|---|---|
| `REPLICATE_API_TOKEN` | — | **required** |
| `OUTPUT_DIR` | `/data` | Where files land |
| `STATE_FILE` | `/data/.state.json` | Tracks processed ids |
| `POLL_INTERVAL` | `900` | Seconds between full passes |
| `HTTP_TIMEOUT` | `60` | Per-request timeout (seconds) |
| `WRITE_METADATA` | `true` | Write `<id>.metadata.json` per prediction |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |

## Where files go

```
data/
├── .state.json
├── gm3qorzdhgbfurvjtvhg6dckhu__00_output.png
├── gm3qorzdhgbfurvjtvhg6dckhu.metadata.json
├── anotherid__00_output.mp4
└── anotherid.metadata.json
```

Filenames are prefixed with the prediction id and an index (`__00_`, `__01_`,
…) so that outputs from many predictions can coexist in one folder without
collisions.

## Caveats (read this)

1. **Replicate purges prediction inputs/outputs ~1 hour after creation** for
   API-created predictions (per
   [docs](https://replicate.com/docs/topics/predictions/lifecycle)). Old
   predictions will return `data_removed=true` and the output URLs will 404.
   This tool preserves the metadata for those but cannot recover the files.
   **Run this container continuously** to catch outputs before they expire.
2. Web-created predictions are queryable for only the last 14 days per docs.
3. The token must belong to the account that owns the predictions.
4. Rate limits apply (Replicate enforces ~600 req/min). The tool uses the
   `seen_ids` set in state to avoid re-processing on each pass, so steady-state
   API usage is minimal: 1 list request per pass + 1 request per new file.
5. A flat directory works fine for a few thousand files. If you generate
   hundreds of thousands, consider migrating to a sub-dirs-by-date layout
   (not implemented; flat layout is the chosen trade-off for simplicity).
6. Downloaded files can be large. Make sure the mounted volume has enough
   space and is on a filesystem that handles many files well.

## Local development

```bash
go run .                              # uses env from your shell or .env
go build -o /tmp/replicate-safe . && /tmp/replicate-safe
```

Hot reloading is not needed — restart after changes.

## How state works

`data/.state.json` contains:

- `last_created_at` — the newest `created_at` seen (used for human reference)
- `seen_ids` — every prediction id processed, so subsequent runs skip them

Delete the file to force a full re-pull. On first start with a token that has
many predictions, the initial pass will walk all pages.

## Why a long-running daemon?

Replicate expires outputs ~1 hour after creation. A daemon that polls every
15 minutes is the simplest reliable way to catch new predictions before they
expire. If you'd rather run it on cron, see the plan file in `.kilo/plans/`
and adapt `main.go` to exit after one pass.

## License

See `LICENSE`.
