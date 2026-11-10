#!/usr/bin/env bash
# Deletes the preview releases of main but the newest KEEP, with their
# tags, and the drafts of previews that a cancelled run left behind:
#
#   .github/scripts/prune-previews.sh [--dry-run] KEEP
#
# A preview is a prerelease tagged vMAJOR.MINOR.PATCH-preview.N
# (.github/workflows/preview.yml); newest is by that version, N compared
# as a number. Releases, other prereleases and anything else are never
# touched. --dry-run prints the gh commands instead of running them. It
# runs gh in the repository's checkout, with GH_TOKEN.
set -euo pipefail

dry=false
if [ "${1:-}" = --dry-run ]; then
  dry=true
  shift
fi
keep=${1:-}
if ! [[ "$keep" =~ ^[1-9][0-9]*$ ]]; then
  echo "usage: prune-previews.sh [--dry-run] KEEP, KEEP at least 1" >&2
  exit 2
fi

run() {
  if $dry; then
    echo "would run: $*"
  else
    echo "$*"
    "$@"
  fi
}

releases=$(gh release list --limit 1000 --json tagName,isDraft,isPrerelease)
# jq's regex of a preview's tag, its numbers captured.
pattern='^v(?<major>[0-9]+)\.(?<minor>[0-9]+)\.(?<patch>[0-9]+)-preview\.(?<n>[0-9]+)$'

# The published previews past the newest KEEP.
old=$(jq -r --arg re "$pattern" --argjson keep "$keep" '
  [.[] | select(.isPrerelease and (.isDraft | not))
    | .tagName as $tag
    | ($tag | capture($re)) as $v
    | {tag: $tag, key: ([$v.major, $v.minor, $v.patch, $v.n] | map(tonumber))}]
  | sort_by(.key) | reverse | .[$keep:][] | .tag' <<< "$releases")
# Drafts have no tag yet: gh creates it when it publishes the release.
drafts=$(jq -r --arg re "$pattern" \
  '.[] | select(.isDraft) | .tagName | select(test($re))' <<< "$releases")

for tag in $old; do
  run gh release delete "$tag" --cleanup-tag --yes
done
for tag in $drafts; do
  run gh release delete "$tag" --yes
done
if [ -z "$old$drafts" ]; then
  echo "No previews to delete"
fi
