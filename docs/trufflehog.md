# TruffleHog Integration (High‑entropy and Regex-based Scanning)

Pilreg can optionally invoke [TruffleHog](https://github.com/trufflesecurity/trufflehog) to scan the contents
of each image layer for high‑entropy strings (e.g. keys) or custom regex patterns. This is useful for
security engineers and penetration testers who want to automate secret discovery across all filesystems
in a container image.

> **Prerequisite**: you must have `trufflehog` installed and available in your `PATH`.

## How to Use

Add the `--trufflehog` (or `-x`) flag to your pilreg command. For example, to scan an image in a local registry:

```bash
# Ensure trufflehog is installed
trufflehog version

# Scan remote registry image with TruffleHog
pilreg --trufflehog 127.0.0.1:5000/test/test
```

If scanning a local tarball, pass `--local`:

```bash
docker pull alpine:latest
docker save alpine:latest -o alpine.tar
pilreg --local alpine.tar --trufflehog
```

## Sample Output

```text
[TRUFFLEHOG] High entropy string blob found in layer sha256:...
  filename: /root/.aws/credentials
  match: AKIAIOSFODNN7EXAMPLE

[TRUFFLEHOG] Regex match for pattern (?i)password\s*[:=]\s*\S+ in layer sha256:...
  filename: /app/config.yml
  match: password: secret123
```

The above output indicates the discovery of AWS keys and plaintext passwords inside the image.

You can adjust TruffleHog’s sensitivity or pass custom regex patterns via environment variables
or a config file as documented in the [TruffleHog docs](https://github.com/trufflesecurity/trufflehog).
