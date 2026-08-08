# Replicate-Safe

Two small Docker containers that continuously download every prediction and
its output files from your Replicate account into a local folder, then serve
a browser UI to browse and preview them.

| Image | Purpose |
|---|---|
| [`nopenix/replicate-safe`](https://hub.docker.com/r/nopenix/replicate-safe) | Daemon — pulls predictions + outputs from Replicate (~15 MB, distroless) |
| [`nopenix/replicate-safe-frontend`](https://hub.docker.com/r/nopenix/replicate-safe-frontend) | Web UI — file list, preview, metadata viewer (~15 MB, distroless) |

![Replicate-Safe — dark theme](img/Screenshot%20Dark.png)
![Replicate-Safe — light theme](img/Screenshot%20Light.png)

## Quick start

```bash
mkdir replicate-safe && cd replicate-safe

cat > .env <<'EOF'
REPLICATE_API_TOKEN=r8_your_token_here
POLL_INTERVAL=900
LOG_LEVEL=info
EOF

curl -O https://raw.githubusercontent.com/NopeNix/Replicate-Safe/main/docker-compose.yml
docker compose up -d
docker compose logs -f
```

Open <http://localhost:8080>. The daemon writes predictions and outputs into
`./data/`, prefixed with the prediction id so nothing collides:

```
data/
├── .state.json
├── 01w9p8j4pdrmw0czsr3bcwtyh4__00_tmpagwigfij.png
├── 01w9p8j4pdrmw0czsr3bcwtyh4.metadata.json
└── ...
```

## docker-compose.yml

```yaml
services:
  replicate-safe:
    image: nopenix/replicate-safe:latest
    restart: unless-stopped
    env_file: [.env]
    environment:
      OUTPUT_DIR: /data
      STATE_FILE: /data/.state.json
    volumes: [./data:/data]

  frontend:
    image: nopenix/replicate-safe-frontend:latest
    restart: unless-stopped
    depends_on: [replicate-safe]
    environment:
      DATA_DIR: /data
      LISTEN_ADDR: ":8080"
    ports: ["8080:8080"]
    volumes: [./data:/data:ro]
```

The frontend mounts the data volume read-only — it can never write to your
backups even if compromised.

## Configuration

### Daemon (`replicate-safe`)

| Variable | Default | Notes |
|---|---|---|
| `REPLICATE_API_TOKEN` | — | **required**, from <https://replicate.com/account/api-tokens> |
| `OUTPUT_DIR` | `/data` | Where files land |
| `STATE_FILE` | `/data/.state.json` | Tracks processed ids |
| `POLL_INTERVAL` | `900` | Seconds between full passes |
| `HTTP_TIMEOUT` | `60` | Per-request timeout (seconds) |
| `WRITE_METADATA` | `true` | Write `<id>.metadata.json` per prediction |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |

### Frontend (`replicate-safe-frontend`)

| Variable | Default | Notes |
|---|---|---|
| `DATA_DIR` | `/data` | Folder the daemon writes to |
| `LISTEN_ADDR` | `:8080` | Where to listen |

## UI features

- **File explorer** with thumbnail, filename, created, model, status, time-to-make, size columns
- **Sort** any column by clicking its header (asc/desc); default = newest first
- **Search** the full `metadata.json` content — prompt text, model name, version, raw API response
- **Preview** native image / video / audio; iframe for text; download link for other types
- **Zoom** the big preview with the mouse wheel, drag to pan, `+`/`−`/`0` keyboard shortcuts
- **Download** or **Convert** (JPG / PNG / GIF / BMP / TIFF) any image
- **Share** to Telegram or WhatsApp, **Copy link**, **Copy image** to clipboard
- **Resizable** split between panels, resizable metadata panel, resizable columns in the explorer
- **Theme** follows system by default; `◐` button cycles auto / light / dark
- **Footer** links to GitHub and Docker Hub

All UI state (panel widths, column widths, zoom, theme, thumbnail size) is
persisted in `localStorage` so your layout survives reloads.

### Frontend API

| Endpoint | Returns |
|---|---|
| `GET /api/predictions?q=<text>` | JSON array, newest-first. One entry per output file. Optional `q` filters by substring match against `metadata.json` content. |
| `GET /api/metadata?id=<id>` | Raw `metadata.json` for a prediction. |
| `GET /file?id=<id>[&file=<name>]` | Streams the output file. |
| `GET /thumb?id=<id>` | 128×128 JPEG thumbnail (server-side resize via imaging). |
| `GET /convert?id=<id>&to=<fmt>` | Re-encodes the image to `<fmt>` (jpg/png/gif/bmp/tiff) and returns it as an attachment. |

## Caveats

1. **Replicate purges prediction inputs/outputs ~1 hour after creation** for
   API-created predictions. Old predictions return `data_removed=true` and
   the output URLs 404 — the metadata is still saved but the files are gone.
   Run the container continuously to catch outputs before they expire.
2. Web-created predictions are queryable for only the last 14 days.
3. The token must belong to the account that owns the predictions.
4. Rate limits apply (~600 req/min). Steady-state usage is minimal: 1 list
   request per pass + 1 request per new file.
5. A flat directory works fine up to a few thousand files. For hundreds of
   thousands, a sub-dirs-by-date layout would be needed.
6. Make sure the mounted volume has enough space — outputs can be large.

## Building from source

Both Docker Hub images are built from this repo by GitHub Actions on every
push to `main`. To hack locally:

```bash
git clone https://github.com/NopeNix/Replicate-Safe.git
cd Replicate-Safe

go run .                              # daemon
docker build -t replicate-safe:dev .

go run ./frontend                     # frontend on :8080
docker build -f frontend/Dockerfile -t replicate-safe-frontend:dev .

docker compose up -d
```

## License

See [`LICENSE`](./LICENSE).
