# pilreg Documentation

Welcome to **pilreg** – a CLI tool for pentesters and security engineers to enumerate and analyze container images.

## Core Features
- **Registry & Local Scanning**: query Docker registries or scan local tarballs to enumerate images, tags, and layers.
- **Whiteout Analysis**: recover files deleted via Docker whiteout markers (see [Whiteout Analysis](whiteout.md)).
- **TruffleHog Integration**: detect high‑entropy strings or regex patterns inside image layers (see [TruffleHog Integration](trufflehog.md)).
- **Config & Secret Scraping**: pull and inspect container config JSON to find embedded credentials, env vars, and metadata.
- **Homebrew & Docker**: install via Homebrew or run directly in a Docker container (see main README).

## Typical Scenarios
- **Pentest with Registry Access**: you’ve compromised registry credentials; use pilreg to quickly enumerate and extract secrets across all repos and tags.
- **CI/CD Security Gates**: integrate pilreg into pipelines to catch secret leakage before image promotion.
- **Incident Response**: post‑compromise, audit container images for hidden artifacts or credentials.

## Getting Started
1. Install or build the binary (see main README).
2. Read individual feature guides:
   - [Whiteout Analysis](whiteout.md)
   - [TruffleHog Integration](trufflehog.md)
   - [Shell Autocomplete](autocomplete.md)
3. Try the examples under `docs/examples/`.

For full CLI reference and advanced options, see the main [README](../README.md).
