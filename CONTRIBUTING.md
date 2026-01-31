# Contributing

This project is designed to be installed by users **without** development tooling.
Developer requirements are intentionally kept out of end-user documentation.

---

## Developer Requirements

- Go 1.24+
- Linux or macOS

---

## Build From Source

```bash
git clone https://github.com/ChrisB0-2/storage-sage.git
cd storage-sage
go build ./cmd/storage-sage
```

---

## Reproducible Static Builds (Release)

Release binaries are built using CI to ensure:

- Static linking
- No runtime dependencies
- Reproducible output

Example:

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
  -trimpath \
  -ldflags="-s -w" \
  -o storage-sage \
  ./cmd/storage-sage
```

---

## Testing

Run unit tests:

```bash
go test ./...
```

Run with race detector:

```bash
go test -race ./...
```

---

## Philosophy

- Users should never need Go
- Releases must be boring and predictable
- Developer tooling stays in CONTRIBUTING.md
