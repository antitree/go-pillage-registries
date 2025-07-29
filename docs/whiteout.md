# Whiteout Analysis (Hidden Files in Docker Layers)

The `--whiteout` feature helps you uncover files that were intentionally deleted
in container image layers ("whiteout files").  Attackers or build pipelines may hide
sensitive content by deleting a file in a later layer, but remnants of that file
can still be recovered from earlier layers.  This feature is especially useful for
security engineers and penetration testers wanting to surface hidden or deleted
secrets in images.

## How It Works

When a file is removed in a Docker layer, the tarball spec uses special whiteout
markers (`.wh.<filename>`) in the next layer to indicate deletion.  By scanning
for these markers and walking back through previous layers, **pilreg** can extract
the original content of deleted files.

## Example Demo

We provide a working example under `docs/examples/Dockerfile.wh.wh` that:

1. Embeds decoy whiteout files (`/.wh.decoyX.txt`) with fake content
2. Creates a real secret file (`/deep/hide/flag.txt`) then deletes it (producing a whiteout)
3. Adds more decoy content to distract analysis

Follow these steps to build, load, and analyze the example image locally:

```bash
# 1. Build the image from the whiteout demo Dockerfile
docker build -f docs/examples/Dockerfile.wh.wh -t whiteout-demo .

# 2. Run pilreg in whiteout mode to recover deleted files
pilreg --whiteout 127.0.0.1:5000/whiteout-demo

# (Optionally push to a local registry:)
docker tag whiteout-demo 127.0.0.1:5000/whiteout-demo
docker push 127.0.0.1:5000/whiteout-demo
pilreg --whiteout 127.0.0.1:5000/whiteout-demo
```

Sample output will indicate the hidden flag file being recovered:

```text
[+] Found original file for whiteout:
    Whited out: /deep/hide/.wh.flag.txt
    Recovered:  /deep/hide/flag.txt
---- File Content ----
Q1RGe3JlYWxfZmxhZ192aWRpZGVuX2hlcmV9
-----------------------
```

Now you can decode the base64 to read the flag:

```bash
echo Q1RGe3JlYWxfZmxhZ192aWRpZGVuX2hlcmV9 | base64 -d
# prints CTF{real_flag_hidden_here}
```

## Animated Demo

You can record this demonstration with [asciinema](https://asciinema.org/) and convert it to a GIF using [agg](https://github.com/asciinema/agg):

```bash
# Record the demo (from repo root)
asciinema rec docs/whiteout-demo.cast -c docs/record_whiteout_demo.sh
agg docs/whiteout-demo.cast docs/images/whiteout-demo.gif --speed 2
```

Below is an animated demo showing the review, build, push, and scan steps:

![Whiteout Demo](images/whiteout-demo.gif)

## Filtering Default Whiteouts

To reduce noise from common temporary or package files, you can supply the flag without any values to apply built-in default patterns:

```bash
# Use --whiteout-filter with no arguments to enable default filters
pilreg --whiteout --whiteout-filter 127.0.0.1:5000/whiteout-demo
```
