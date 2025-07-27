#!/bin/bash
set -euo pipefail

if [[ $# -ne 1 ]]; then
  echo "Usage: $0 <docker-image-tarball>"
  exit 1
fi

IMAGE_TAR="$1"
WORKDIR="whiteout_recovery_tmp"

# Cleanup and prep
rm -rf "$WORKDIR"
mkdir -p "$WORKDIR"
cd "$WORKDIR"

echo "[*] Extracting image tarball..."
tar -xf "../$IMAGE_TAR"

echo "[*] Reading layer list..."
mapfile -t LAYERS < <(jq -r '.[0].Layers[]' manifest.json)

echo "[*] Unpacking layers..."
for i in "${!LAYERS[@]}"; do
  LAYER_FILE="${LAYERS[$i]}"
  mkdir -p "layer_$i"
  tar -xf "$LAYER_FILE" -C "layer_$i"
done

echo "[*] Scanning for whiteout files and recovering originals..."

for i in "${!LAYERS[@]}"; do
  CURRENT_LAYER="layer_$i"
  echo "[*] Checking $CURRENT_LAYER for whiteouts..."

  while IFS= read -r WH_FILE; do
    REL_PATH="${WH_FILE#$CURRENT_LAYER/}"              # e.g., test/.wh.flag.txt
    DIRNAME="$(dirname "$REL_PATH")"
    BASENAME="$(basename "$REL_PATH")"
    ORIGINAL_NAME="${BASENAME#.wh.}"
    ORIGINAL_PATH="$DIRNAME/$ORIGINAL_NAME"            # e.g., test/flag.txt

    PREV_INDEX=$((i - 1))
    if (( PREV_INDEX < 0 )); then
      continue
    fi

    CANDIDATE_PATH="layer_$PREV_INDEX/$ORIGINAL_PATH"

    if [[ -f "$CANDIDATE_PATH" ]]; then
      echo "[+] Found original file for whiteout:"
      echo "    Whited out: $WH_FILE"
      echo "    Recovered:  $CANDIDATE_PATH"
      echo "---- File Content ----"
      cat "$CANDIDATE_PATH"
      echo "-----------------------"
    fi

  done < <(find "$CURRENT_LAYER" -type f -name ".wh.*")

done

