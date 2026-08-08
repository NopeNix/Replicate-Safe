# Replicate-Safe

**Don't lose what you paid for.** A pair of tiny Docker containers that
continuously download every prediction (and its output files) created under
your Replicate account to a local folder, then let you browse and preview
them in a clean web UI. Powered by a single env var: your API token.

| Image | What it does |
|---|---|
| [`nopenix/replicate-safe`](https://hub.docker.com/r/nopenix/replicate-safe) | Daemon that pulls predictions + outputs from Replicate (~15 MB, distroless) |
| [`nopenix/replicate-safe-frontend`](https://hub.docker.com/r/nopenix/replicate-safe-frontend) | Read-only web UI: file list + image/video/audio preview + metadata viewer (~15 MB, distroless) |

## Quick start (docker compose)

```bash
mkdir replicate-safe && cd replicate-safe

# 1. Set your token (get one at https://replicate.com/account/api-tokens)
cat > .env <<'EOF'
REPLICATE_API_TOKEN=r8_your_token_here
POLL_INTERVAL=900
LOG_LEVEL=info
EOF

# 2. Start both containers. Every 15 minutes the daemon pulls new
#    predictions + outputs into ./data. The frontend reads from the
#    same folder.
docker compose up -d

# 3. Watch it work
docker compose logs -f

# 4. Open the UI
open http://localhost:8080      # macOS
xdg-open http://localhost:8080  # Linux
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

Both images are published on Docker Hub; you don't need to clone this repo.
Drop this file next to a `.env`:

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

  frontend:
    image: nopenix/replicate-safe-frontend:latest
    container_name: replicate-safe-frontend
    restart: unless-stopped
    depends_on:
      - replicate-safe
    environment:
      DATA_DIR: /data
      LISTEN_ADDR: ":8080"
    ports:
      - "8080:8080"
    volumes:
      - ./data:/data:ro
```

Then `docker compose pull && docker compose up -d`. The frontend will be at
<http://localhost:8080>.

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

## The frontend

`replicate-safe-frontend` is a read-only browser UI that reads from the same
`./data` folder the daemon writes to. It is a separate container (and
separate Docker Hub image) so you can run it without exposing the daemon, or
point it at any folder produced by a different `replicate-safe` instance.

Open <http://localhost:8080> and you get a split view:

- **Left:** file explorer. One row per output file. Columns: filename, model,
  status, time-to-make, size. Sorted newest-first. JSON metadata sidecars and
  the state file are hidden. Filter by typing in the search box. Arrow keys
  navigate; click selects.
- **Right:** preview pane. Images, video, and audio use native browser
  players. Text files render in an iframe. Anything else shows a download
  link. Below it, a collapsible `metadata.json` viewer shows the full
  prediction (inputs, model, version, status, timestamps, raw API response)
  pretty-printed.
- **Theme:** follows your system theme (`prefers-color-scheme`) by default.
  The small `◐` button in the top-right cycles auto → light → dark. Choice is
  persisted in `localStorage`.

The frontend only reads from the bind-mounted volume (`:ro`), so even if the
daemon's container is compromised the frontend can't escalate into writes.

### Frontend environment variables

| Variable | Default | Notes |
|---|---|---|
| `DATA_DIR` | `/data` | Folder the daemon writes to |
| `LISTEN_ADDR` | `:8080` | Where to listen (use `:80` behind a reverse proxy) |
| `CACHE_TTL` | `5` | Seconds to cache the listing before re-scanning the disk |

### Frontend API

The UI is a thin client over a tiny JSON API; you can use it from scripts:

| Endpoint | Returns |
|---|---|
| `GET /api/predictions` | JSON array, newest-first. One entry per output file. Fields: `id`, `filename`, `size`, `model`, `version`, `status`, `created_at`, `completed_at`, `time_to_make`, `mime`, `preview_kind`. |
| `GET /api/metadata?id=<id>` | Raw `metadata.json` for a prediction. `404` if missing. |
| `GET /file?id=<id>` | Streams the first output file for a prediction. Add `&file=<name>` to pick a specific output when a prediction produced several. |

## What's inside the daemon

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

Only needed if you want to hack on it. Both Docker Hub images are built from
this repo by GitHub Actions on every push to `main`.

```bash
git clone https://github.com/NopeNix/Replicate-Safe.git
cd Replicate-Safe

# Daemon
go run .                            # uses env from your shell
docker build -t replicate-safe:dev .

# Frontend
go run ./frontend                   # serves on :8080, reads DATA_DIR=/data by default
docker build -f frontend/Dockerfile -t replicate-safe-frontend:dev .

# Run both with compose (pointing at the locally-built images)
docker compose up -d
```

## License

See [`LICENSE`](./LICENSE).
