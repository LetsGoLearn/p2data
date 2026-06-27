#!/usr/bin/env bash
# Download a privacy-filter GGUF model into ./models.
#
# Usage:
#   scripts/download-model.sh [q8|f16]
#
# q8 (default) is ~1.5x smaller and matched f16 on 99.7% of token labels in the
# upstream benchmark; f16 is the reference precision.
set -euo pipefail

VARIANT="${1:-q8}"
case "$VARIANT" in
  q8)  FILE="privacy-filter-q8.gguf" ;;
  f16) FILE="privacy-filter-f16.gguf" ;;
  *) echo "unknown variant: $VARIANT (use q8 or f16)" >&2; exit 1 ;;
esac

REPO="LocalAI-io/privacy-filter-GGUF"
URL="https://huggingface.co/${REPO}/resolve/main/${FILE}?download=true"
DIR="$(cd "$(dirname "$0")/.." && pwd)/models"
DEST="${DIR}/${FILE}"

mkdir -p "$DIR"
if [ -f "$DEST" ]; then
  echo "already present: $DEST"
  exit 0
fi

echo "downloading $FILE from $REPO ..."
curl -fL --progress-bar -o "${DEST}.part" "$URL"
mv "${DEST}.part" "$DEST"
echo "saved to $DEST"
echo
echo "Run with:  REDACTOR_MODEL_PATH=$DEST"
