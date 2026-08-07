# Replicate-Safe

**Don't lose what you paid for.** A tiny Docker container that continuously
downloads every prediction (and its output files) created under your Replicate
account to a local folder. Powered by a single env var: your API token.

Image on Docker Hub: **[`nopenix/replicate-safe`](https://hub.docker.com/r/nopenix/replicate-safe)** (~15 MB, distroless).

## Quick start (docker compose)

```bash
mkdir replicate-safe && cd replicate-safe

# 1. Set your token (get one at https://replicate.com/account/api-tokens)
cat > .env <<'EOF'
REPLICATE_API_TOKEN=r8_your_token_here
POLL_INTERVAL=900
LOG_LEVEL=info
EOF

# 2. Start it. Every 15 minutes it pulls new predictions + outputs into ./data.
docker compose up -d

# 3. Watch it work
docker compose logs -f
```

That's it. Predictions and their output files appear as a flat dump under
`./data/`, prefixed with the prediction id so nothing collides:

```
data/
├── .state.json
├── 01w9p8j4pdrmw0czsr3bcwtyh4__00_tmpagwigfij.png
├── 01w9p8j4pdrmw0czsr3bcwtyh4.metadata.json
├── 7cc8srxcenrmw0czh7fv92jha8__00_tmpjzq4oox6.svg
└── ...
```

## `docker-compose.yml`

The image is published as [`nopenix/replicate-safe`](https://hub.docker.com/r/nopenix/replicate-safe),
so you don't need to clone this repo. Drop this file next to a `.env`:

```yaml
services:
  replicate-safe:
    image: nopenix/replicate-safe:latest
    container_name: replicate-safe
    restart: unless-stopped
    env_file:
      - .env
    environment:
      OUTPUT_DIR: /data
      STATE_FILE: /data/.state.json
    volumes:
      - ./data:/data
```

> **One-time `chmod`:** `replicate-safe` runs as uid 65532 (nonroot), so the
> host directory needs to be writable by it. `chmod 777 ./data` is fine for a
> single-tenant backup volume; alternatives are `chown 65532 ./data` or
> pre-creating files with the right uid.

Then `docker compose pull && docker compose up -d`.

## Configuration

All settings are environment variables. Pulled in via the `.env` file or the
`environment:` block in compose.

| Variable | Default | Notes |
|---|---|---|
| `REPLICATE_API_TOKEN` | — | **required**, from <https://replicate.com/account/api-tokens> |
| `OUTPUT_DIR` | `/data` | Where files land inside the container |
| `STATE_FILE` | `/data/.state.json` | Tracks processed ids |
| `POLL_INTERVAL` | `900` | Seconds between full passes |
| `HTTP_TIMEOUT` | `60` | Per-request timeout (seconds) |
| `WRITE_METADATA` | `true` | Write `<id>.metadata.json` per prediction |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |

### Pulling a specific version

The image is tagged by branch, short SHA, and semver. Pin to a release for
predictable behavior:

```yaml
image: nopenix/replicate-safe:v0.1.0     # specific release
image: nopenix/replicate-safe:latest     # newest main
image: nopenix/replicate-safe:main       # same as latest
```

## What's inside the container

A single static Go binary on `gcr.io/distroless/static:nonroot`. It:

1. On start (and every `POLL_INTERVAL` seconds), lists every prediction your
   token can see via the Replicate HTTP API.
2. For each prediction it hasn't seen before, downloads every URL in
   `output` into `OUTPUT_DIR`, prefixed with `<prediction-id>__<index>_`.
3. Writes a `<prediction-id>.metadata.json` sidecar with inputs, model,
   version, status, timestamps, and the raw API response (toggleable).
4. Persists the set of seen ids in `STATE_FILE` so subsequent passes skip
   everything already downloaded.

State is saved atomically (tmp file + rename) after every pass, so killing
the container mid-sync is safe.

## Caveats (read this)

1. **Replicate purges prediction inputs/outputs ~1 hour after creation** for
   API-created predictions. Old predictions return `data_removed=true` and
   the output URLs 404 — the metadata is still saved but the files are gone.
   **Run the container continuously** to catch outputs before they expire.
2. Web-created predictions are queryable for only the last 14 days.
3. The token must belong to the account that owns the predictions.
4. Rate limits apply (~600 req/min). Steady-state usage is minimal: 1 list
   request per pass + 1 request per new file.
5. A flat directory works fine up to a few thousand files. For hundreds of
   thousands, consider a sub-dirs-by-date layout (not implemented).
6. Downloaded files can be large. Make sure the mounted volume has enough
   space.

## Running on a schedule instead of as a daemon

If you'd rather invoke `nopenix/replicate-safe` from cron / systemd timer /
k8s CronJob, the binary exits cleanly after one pass and updates state on
shutdown. Just set `POLL_INTERVAL` to a very large value (or fork the
source to remove the loop — not necessary in practice).

## Building from source

Only needed if you want to hack on it. The image on Docker Hub is built from
this repo by GitHub Actions on every push to `main`.

```bash
git clone https://github.com/NopeNix/Replicate-Safe.git
cd Replicate-Safe
go run .                       # uses env from your shell
docker build -t replicate-safe:dev .
docker run --rm -e REPLICATE_API_TOKEN=r8_xxx -v "$(pwd)/data:/data" replicate-safe:dev
```

## License

See [`LICENSE`](./LICENSE).
