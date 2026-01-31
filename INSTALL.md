# Installation

Storage-Sage is distributed as a standalone Linux binary.
Go is **not** required unless you are building from source.

---

## 1. Quick Install (Recommended)

Download the prebuilt static binary for your platform.

### linux/amd64

```bash
curl -LO https://github.com/ChrisB0-2/storage-sage/releases/latest/download/storage-sage-linux-amd64
chmod +x storage-sage-linux-amd64
sudo mv storage-sage-linux-amd64 /usr/local/bin/storage-sage
storage-sage --help
```

### linux/arm64

```bash
curl -LO https://github.com/ChrisB0-2/storage-sage/releases/latest/download/storage-sage-linux-arm64
chmod +x storage-sage-linux-arm64
sudo mv storage-sage-linux-arm64 /usr/local/bin/storage-sage
storage-sage --help
```

### Optional: verify checksums

```bash
curl -LO https://github.com/ChrisB0-2/storage-sage/releases/latest/download/SHA256SUMS
sha256sum -c SHA256SUMS --ignore-missing
```

---

## 2. Docker Install (No Host Changes)

```bash
docker run --rm ghcr.io/chrisb0-2/storage-sage --help
```

---

## 3. Build From Source (Developers Only)

Requires Go 1.24+

```bash
git clone https://github.com/ChrisB0-2/storage-sage.git
cd storage-sage
go build ./cmd/storage-sage
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup.
