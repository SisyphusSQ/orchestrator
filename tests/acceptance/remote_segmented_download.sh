#!/usr/bin/env bash

set -euo pipefail

if [[ $# != 3 ]]; then
  echo "usage: remote_segmented_download.sh URL DEST PARALLELISM" >&2
  exit 2
fi

url=$1
dest=$2
parallelism=$3
[[ "$dest" == /* ]] || { echo "DEST must be absolute" >&2; exit 2; }
[[ "$parallelism" =~ ^[1-9][0-9]*$ ]] || { echo "PARALLELISM must be positive" >&2; exit 2; }
[[ ! -e "$dest" ]] || { echo "DEST already exists: $dest" >&2; exit 1; }

content_length=$(curl -fsSIL "$url" | tr -d '\r' | awk 'tolower($1) == "content-length:" {size=$2} END {print size}')
[[ "$content_length" =~ ^[1-9][0-9]*$ ]] || { echo "cannot determine Content-Length" >&2; exit 1; }

chunk_bytes=$((4 * 1024 * 1024))
chunk_count=$(((content_length + chunk_bytes - 1) / chunk_bytes))
parts_dir=$dest.parts
assembled=$dest.assembled
[[ ! -e "$parts_dir" && ! -e "$assembled" ]] || { echo "stale segmented download state" >&2; exit 1; }
install -d -m 0700 "$parts_dir"

download_part() {
  local index=$1 start end expected part partial actual
  start=$((index * chunk_bytes))
  end=$((start + chunk_bytes - 1))
  ((end < content_length)) || end=$((content_length - 1))
  expected=$((end - start + 1))
  part=$(printf '%s/%06d.part' "$parts_dir" "$index")
  partial=$part.partial
  if ! curl --silent --show-error --fail --location --retry 5 --range "$start-$end" --output "$partial" "$url"; then
    echo "range download failed index=$index start=$start end=$end" >&2
    return 1
  fi
  actual=$(stat -c %s "$partial")
  [[ "$actual" == "$expected" ]] || {
    echo "range size mismatch index=$index expected=$expected actual=$actual" >&2
    return 1
  }
  mv "$partial" "$part"
  printf 'PART index=%s bytes=%s\n' "$index" "$actual"
}
export -f download_part
export url content_length chunk_bytes parts_dir
seq 0 $((chunk_count - 1)) | xargs -P "$parallelism" -n 1 bash -c 'download_part "$1"' _

: >"$assembled"
for index in $(seq 0 $((chunk_count - 1))); do
  part=$(printf '%s/%06d.part' "$parts_dir" "$index")
  [[ -f "$part" ]] || { echo "missing part: $part" >&2; exit 1; }
  cat "$part" >>"$assembled"
done
actual=$(stat -c %s "$assembled")
[[ "$actual" == "$content_length" ]] || { echo "assembled size mismatch expected=$content_length actual=$actual" >&2; exit 1; }
mv "$assembled" "$dest"
sha256sum "$dest"
printf 'PASS bytes=%s chunks=%s parallelism=%s dest=%s parts=%s\n' \
  "$content_length" "$chunk_count" "$parallelism" "$dest" "$parts_dir"
