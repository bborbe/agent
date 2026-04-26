#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail
set -o errtrace

# Parse mode flag
MODE="interactive"
if [ $# -ge 1 ]; then
  case "$1" in
    --dry-run)
      MODE="dry-run"
      ;;
    --yes)
      MODE="yes"
      ;;
    *)
      echo "Error: Invalid flag '$1'" >&2
      echo "Usage: $0 [--dry-run | --yes]" >&2
      exit 1
      ;;
  esac
fi

ROOTDIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOTDIR"

# Detect branch
BRANCH=$(git rev-parse --abbrev-ref HEAD)

# Map branch to environment name
case "$BRANCH" in
    master)
        ENV="master"
        ;;
    dev)
        ENV="dev"
        ;;
    prod)
        ENV="prod"
        ;;
    *)
        echo "Error: Unknown branch '$BRANCH'. Expected: master, dev, or prod" >&2
        exit 1
        ;;
esac

OUTPUT_FILE="/tmp/agent-${ENV}-buca.log"
SCREEN_SESSION="agent-${ENV}-buca"

echo "BRANCH: $BRANCH"
echo "ENV: $ENV"
echo "OUTPUT_FILE: $OUTPUT_FILE"
echo "SCREEN_SESSION: $SCREEN_SESSION"
echo "ROOTDIR: $ROOTDIR"

if [ "$MODE" = "dry-run" ]; then
  echo ""
  echo "DRY-RUN: Would run make buca with command:"
  echo "  screen -dmS \"$SCREEN_SESSION\" bash -c \"cd '$ROOTDIR' && make buca > $OUTPUT_FILE 2>&1; echo 'Done. Exit: \\\$?' >> $OUTPUT_FILE\""
  echo ""
  echo "To monitor progress (after running with --yes):"
  echo "  tail -f $OUTPUT_FILE"
  echo ""
  echo "To reattach to screen (after running with --yes):"
  echo "  screen -r $SCREEN_SESSION"
  exit 0
fi

if [ "$MODE" = "interactive" ]; then
  echo ""
  read -p "Are you sure? " -n 1 -r
  echo
  if [[ ! $REPLY =~ ^[Yy]$ ]]; then
    exit 0
  fi
fi

# Start screen session with the buca command
screen -dmS "$SCREEN_SESSION" bash -c "cd '$ROOTDIR' && make buca > $OUTPUT_FILE 2>&1; echo 'Done. Exit: \$?' >> $OUTPUT_FILE"

echo ""
echo "Screen session started: $SCREEN_SESSION"
echo ""
echo "To monitor progress:"
echo "  tail -f $OUTPUT_FILE"
echo ""
echo "To reattach to screen:"
echo "  screen -r $SCREEN_SESSION"
echo ""

sleep 2

# Show initial output
if [ -f "$OUTPUT_FILE" ]; then
    echo "Initial output:"
    tail -10 "$OUTPUT_FILE"
fi
