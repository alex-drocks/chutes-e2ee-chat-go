# Chutes E2EE Chat Go

This is the Wails + Go port of `chutes-e2ee-chat` https://github.com/alex-drocks/chutes-e2ee-chat. The frontend is the existing statically exported Next.js renderer; the Electron main process and preload API are being replaced with Wails bindings backed by Go.

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

## GitHub Automation

This port builds and publishes Windows artifacts only.

- CI runs on pull requests and pushes to `main`.
- CI installs frontend dependencies with `npm ci`, runs `npm audit --audit-level=high`, checks `go mod tidy`, runs `go test ./...`, runs `wails doctor`, builds the Windows executable, and uploads it as a workflow artifact.
- Every push to `main` also runs the Release workflow and publishes a GitHub prerelease with a unique `main-<run>-<sha>` tag, the Windows x64 executable, and `SHA256SUMS.txt`.
- Versioned releases still run on `v*` tags or manual workflow dispatch.
- Manual releases can bump `patch`, `minor`, or `major`, or publish/rebuild an explicit version.
- Dependabot checks frontend npm packages, Go modules, and GitHub Actions weekly.

## Notes

Release artifacts are unsigned for now. Windows code signing can be layered into `.github/workflows/release.yml` once signing certificates and secrets are available.
