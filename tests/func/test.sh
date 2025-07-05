#!/bin/bash
set -uo pipefail

BIN=pilreg
REG="localhost:5000"

tests=(
  "-s -r skaffold -o ./tmp/test1 -w"
  "-r skaffold -o ./tmp/test2 -x"
  "-r skaffold -w"
  "-r skaffold -c /tmp/cache"
  "-r skaffold"
  "-s -r keys -o ./tmp/test6 -w"  # New test for the keys image
)

cleanup() {
  echo "Cleaning up old output..."
  rm -rf ./tmp
}

cleanup
mkdir -p tmp

summary=()

for i in "${!tests[@]}"; do
  test_id="test$((i + 1))"
  out_dir="./tmp/$test_id"
  mkdir -p "$out_dir"

  echo "Running $test_id: ${tests[$i]}"

  before=$(mktemp)
  find "$out_dir" > "$before"

  if $BIN "$REG" ${tests[$i]} > "$out_dir/stdout.log" ; then
    result="PASS"
  else
    result="FAIL"
  fi

  after=$(mktemp)
  find "$out_dir" > "$after"

  change_count=$(diff -u "$before" "$after" | grep -E '^\+' | grep -v '^+++' | wc -l)

  summary+=("$test_id: $result, $change_count files changed")

  rm "$before" "$after"
done

echo
echo "=== Summary ==="
for line in "${summary[@]}"; do
  echo "$line"
done
