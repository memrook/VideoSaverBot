# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
# Build
go build -o videosaverbot

# Run
TELEGRAM_BOT_TOKEN="your_token" ./videosaverbot
./videosaverbot -token="your_token" -debug=true -concurrent=10

# Dependencies
go mod tidy

# Run tests
go test ./...
```

**External dependencies:**
- `yt-dlp` — required for YouTube Shorts support; bot logs a warning and disables YouTube if not found
- `ffprobe` (ffmpeg) — required for video dimension detection before sending to Telegram; bot falls back to `bot.Send` without dimensions if not found

## Architecture

The project is two packages:

**`main.go`** — Telegram bot entry point and message router:
- Parses CLI flags (`-token`, `-debug`, `-concurrent`)
- Falls back to `TELEGRAM_BOT_TOKEN` env var; `BOT_ADMIN_ID` env var sets the admin user ID for `/stats`
- Each incoming message is handled in a goroutine tracked by `sync.WaitGroup` for graceful shutdown
- `handleMessage` enforces per-user concurrency (one active download per user via `activeUsers sync.Map`), then acquires the global semaphore with queue feedback, then dispatches to the appropriate downloader
- Downloads use `context.WithTimeout(context.Background(), 3*time.Minute)` — independent of the shutdown context so SIGTERM doesn't cancel in-flight downloads
- SIGTERM/SIGINT triggers graceful shutdown: waits up to 30 s for active downloads to finish
- `sendVideo` runs `ffprobe` to get video dimensions and sends via a raw multipart HTTP request with explicit `width`/`height` (tgbotapi v5.5.1 `VideoConfig` has no Width/Height fields); falls back to `bot.Send` if ffprobe is unavailable
- A semaphore channel (`downloadSemaphore`) limits concurrent downloads (default 5); non-blocking acquire detects full semaphore and shows queue position to waiting users
- Global atomic counters: `queuedCount` (waiting for semaphore), `runningCount` (actively downloading), `statTotal`, `statErrors`
- Periodic cleanup goroutine removes temp files older than 24h from `temp_videos/`

**`downloader/downloader.go`** — All download logic:
- Public entry points: `DownloadInstagramVideo`, `DownloadTwitterVideo`, `DownloadTikTokVideo`, `DownloadFacebookVideo`, `DownloadYouTubeVideo` — each takes `(ctx context.Context, url string, userID int64) (string, error)` and returns a local file path
- `ctx` propagates through all 16 internal functions; all HTTP requests use `NewRequestWithContext`; yt-dlp uses `exec.CommandContext`
- Primary download path: `snapsaveDownload` → platform-specific `getSnapsaveVideoURL*` functions that hit snapsave.app/snaptik.app APIs with token extraction and custom decryption (`decodeSnapApp`, `decryptSnapSave`, `decryptSnaptik`)
- Fallback path: `fallbackDownload` is called both when `getSnapsaveVideoURL` fails AND when `downloadMedia` fails (e.g. 403 from rapidcdn.app CDN)
- Per-platform fallbacks: DDInstagram for Instagram, VXTwitter for Twitter, tikmate.online for TikTok
- YouTube uses `yt-dlp` via `exec.CommandContext`
- Temp files are stored in `temp_videos/<userID>/` with random hex IDs to avoid collisions; `tempDirMutex` protects directory creation

## Key design patterns

- Each platform has a primary (snapsave) + fallback strategy; fallback is triggered by both API lookup failures and file download failures (e.g. CDN 403)
- The decryption logic in `decodeSnapApp` / `decryptSnapSave` / `decryptSnaptik` reverse-engineers the obfuscation used by snapsave.app/snaptik.app HTML responses
- Video send uses ffprobe + raw HTTP multipart to pass explicit dimensions because Telegram Bot API auto-detection fails for some video containers (isom/iso4), causing square 320×320 display
- Temp file cleanup happens both immediately after send (in `sendVideo`) and periodically via `startPeriodicCleanup`
- Connection monitoring pings `GetMe` every 10 minutes and reconnects on failure
- Two independent context scopes: `shutdownCtx` (signal-based, controls the message loop) and per-download `context.Background()+timeout` (not a child of shutdownCtx)
