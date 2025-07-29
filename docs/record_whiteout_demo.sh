#!/usr/bin/env bash
set -euo pipefail

sed -n '1,200p' docs/examples/Dockerfile.wh.wh
sleep 3

# # A demo script to build, push, and scan the whiteout example image
REGISTRY="localhost:5000"
REG_NAME="asciinema-registry"

echo "== Scanning for whiteout files =="
pilreg --whiteout $REGISTRY -r whiteoutdemo

cat results/$REGISTRY/whiteoutdemo/latest/deep/hide/flag.txt.4 

cat results/$REGISTRY/whiteoutdemo/latest/deep/hide/flag.txt.4 | base64 -d



