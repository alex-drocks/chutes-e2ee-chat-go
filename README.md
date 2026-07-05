# Chutes E2EE Chat Go

This is the Wails + Go port of `chutes-e2ee-chat`. The frontend is the existing statically exported Next.js renderer; the Electron main process and preload API are being replaced with Wails bindings backed by Go.

## Why Wails

Wails wraps a Go backend and web frontend into one desktop application without bundling Electron's Chromium runtime. Wails v2 is the stable line, while v3 is still alpha upstream, so this port starts on Wails v2.

## Current Port Surface

- Wails v2 application scaffold.
- Existing Next renderer under `frontend/`.
- `window.chutes` compatibility bridge for the renderer.
- Go backend bindings for chat, abort, model discovery, model stats, web search, clipboard-image status, and API-key settings.
- Go Chutes E2EE transport for ML-KEM-768, HKDF-SHA256, gzip, ChaCha20-Poly1305, nonce discovery, invoke, and streaming event emission.
- Local encrypted credential storage compatible with the Electron fallback envelope (`credentials.enc` plus `credentials.key`).

## Requirements

- Go 1.26 or newer. This port uses the standard-library `crypto/mlkem` package.
- Wails v2 CLI.
- Node.js and npm for the Next renderer.
- WebView2 runtime on Windows.

Install Wails:

```powershell
go install github.com/wailsapp/wails/v2/cmd/wails@latest
wails doctor
```

Install frontend dependencies:

```powershell
npm --prefix frontend install
```

Run in development:

```powershell
wails dev
```

Build:

```powershell
wails build
```

## Notes

The local shell used to create this scaffold did not have `go`, `wails`, or `bun` on PATH, so the project could not be compiled in this environment yet. After installing Go/Wails/npm dependencies, run `wails doctor`, `npm --prefix frontend run build`, and `wails build`.
