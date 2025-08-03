# go-registry-pillage: Origins, Enhancements, and a Deep Dive into Whiteout Security

In this post, we trace the history and evolution of **go-registry-pillage** (aka *pilreg*), from its inception by Josh Makinen of NCC Group to the latest feature-packed updates. We’ll cover new brute‑force registry enumeration, integration with TruffleHog for deep secret hunting, and an in‑depth look at the powerful `--whiteout` and `--whiteout-filter` capabilities inspired by Jon Johnson Jr. during his time at Chainguard.

---

## Origins and Acknowledgments

go-registry-pillage was originally created by **Josh Makinen** (@jmakinen-ncc) at the NCC Group as a lightweight Go-based CLI to audit container registries and image tarballs for hidden artifacts, deleted files, and secrets. Thanks to Josh’s solid foundation, the community has since extended the tool under the familiar name **pilreg**, documented here in the [pilreg docs](/docs/README.md).

Special shoutouts:
- **@jmakinen-ncc** for laying the groundwork with go-registry-pillage.
- **@jonjohnsonjr** for the inspiration behind the whiteout recovery feature (and his work on Crane) while at Chainguard.

---

## 1. Brute‑Force Registry Enumeration (Catalog API Fallback)

Container registries often expose a catalog API, but some installations disable or lock down this endpoint. To work around that, pilreg now ships with a built‑in list of popular repository prefixes and image names, allowing it to *brute‑force* common targets when the catalog call fails.

Under the hood, pilreg embeds a JSON configuration (`default_config.json`) containing arrays of repository prefixes (`Repos`) and image names (`Names`). When:

```go
if err := crane.Catalog(reg, options...); err != nil {
    LogWarn("Catalog API not available. Falling back to brute force enumeration.")
    repos = bruteForceTags(reg, defaultConfigData, options...)
}
```

…pilreg iterates over each prefix/name combination, checks for a valid manifest, and resurrects any hits. Typical defaults include popular terms like `library/ubuntu`, `library/nginx`, `busybox`, and `alpine`, but you can customize the list by modifying `default_config.json` in the source.

This fallback ensures that even hardened registries can be probed for known images, giving pentesters and defenders alike a fighting chance to discover what’s really hosted.

---

## 2. TruffleHog Integration for Deep Artifact Hunting

Pilreg already supported basic secret scraping from container configs, but many teams rely on [TruffleHog](https://github.com/trufflesecurity/trufflehog) to detect high‑entropy strings, AWS keys, and custom regex patterns buried deep in layers. The latest updates wire in TruffleHog as an optional scanning pass:

```bash
# Invoke pilreg with --trufflehog to run embedded TruffleHog rules against each layer
pilreg --trufflehog registry.example.com/myapp:latest
```

Configuration is read from your TruffleHog YAML (just as if you ran `trufflehog` directly), and findings are annotated alongside normal pilreg output. For more details and tuning, see the [TruffleHog Integration guide](trufflehog.md).

---

## 3. The `--whiteout` Feature: Recovering Deleted Files

### 3.1 Why Whiteouts Matter

In Docker’s layered filesystem (OverlayFS), removing a file in a higher layer doesn’t erase its data in lower layers. Instead, the tarball uses a *whiteout* marker: a zero-byte file prefixed with `.wh.` and, for opaque directory deletions, a special entry called `.wh..wh..opq`.

Attackers (or careless CI pipelines) can abuse this mechanism to hide secrets: write a password file in one layer, then delete it in the next layer. Standard tools simply respect the whiteout and omit the file, but its content lingers below the surface.

### 3.2 How Pilreg Tracks Whiteouts

The `--whiteout` pass in pilreg scans every layer’s tar archive, looking for both file‑specific and opaque whiteout markers:

1. **File Deletion**: `.wh.<filename>` indicates `<filename>` was removed at this layer.
2. **Opaque Opacity**: `.wh..wh..opq` means “delete everything under this directory.”

Pilreg then walks back through the image’s layer history to extract the original file content and prints a full recovery report:

```text
[+] Found original file for whiteout:
    Whited out: /etc/secret/.wh.config.yaml
    Recovered:  /etc/secret/config.yaml
---- File Content ----
supersecret: mypassword123
-----------------------
```

Refer to the [Whiteout Analysis guide](whiteout.md) for a worked example and animated demo.

### 3.3 Filtering Noise with `--whiteout-filter`

Not every whiteout is malicious. OS package managers and build scripts routinely delete temp files or stale artifacts. To reduce false positives, pilreg now supports `--whiteout-filter`, which applies built‑in filters (you can also supply your own patterns) to ignore common paths like `lost+found`, cache directories, or ephemeral build files.

```bash
# Apply default whiteout filters to suppress benign deletions
pilreg --whiteout --whiteout-filter registry.example.com/myapp:latest
```

This feature was directly inspired by discussions with Jon Johnson Jr. (@jonjohnsonjr) while at Chainguard, and it dramatically cuts down on noise when analyzing large, multi‑layer images.

---

## Getting Started with the New Features

1. **Update or Install pilreg** from source as usual:
   ```bash
   git clone https://github.com/your-org/pilreg.git
   cd pilreg
   go install ./cmd/pilreg
   ```

2. **Try Brute‑Force Enumeration** on a registry with catalog API disabled:
   ```bash
   pilreg --workers 20 registry.example.com
   ```

3. **Run TruffleHog Scans**:
   ```bash
   pilreg --trufflehog registry.example.com/myrepo:latest
   ```

4. **Recover Deleted Files** with Whiteout:
   ```bash
   pilreg --whiteout --whiteout-filter registry.example.com/myrepo:latest
   ```

For further configuration details, see the [pilreg documentation](README.md) and the individual feature pages under `docs/`.

---

## Conclusion

With these updates, go-registry-pillage (pilreg) continues to mature into a comprehensive registry‑ and image‑ auditing toolkit, blending registry enumeration, secret hunting, and filesystem forensics under one roof. Huge thanks to Josh Makinen for the original vision, Jon Johnson Jr. for whiteout insights, and the wider community for feedback and contributions.

Stay secure, keep pillaging safely, and let us know what you discover!
