#!/usr/bin/env bash
# Tests prune-previews.sh against a fake gh that lists releases and logs
# what it is asked to delete:
#
#   .github/scripts/prune-previews-test.sh
set -euo pipefail

script="$(cd "$(dirname "$0")" && pwd)/prune-previews.sh"
dir=$(mktemp -d)
trap 'rm -rf "$dir"' EXIT

cat > "$dir/releases.json" <<'EOF'
[
  {"tagName": "v0.4.1-preview.11", "isDraft": true, "isPrerelease": true},
  {"tagName": "v0.4.1-preview.10", "isDraft": false, "isPrerelease": true},
  {"tagName": "v0.4.1-preview.8", "isDraft": false, "isPrerelease": true},
  {"tagName": "v0.4.1-preview.7", "isDraft": false, "isPrerelease": true},
  {"tagName": "v0.5.0-beta.1", "isDraft": false, "isPrerelease": true},
  {"tagName": "v0.4.1-preview.6", "isDraft": false, "isPrerelease": true},
  {"tagName": "v0.4.1-preview.5", "isDraft": false, "isPrerelease": true},
  {"tagName": "v0.4.1-preview.4", "isDraft": false, "isPrerelease": true},
  {"tagName": "v0.4.0", "isDraft": false, "isPrerelease": false},
  {"tagName": "v0.4.1-preview.9", "isDraft": false, "isPrerelease": false},
  {"tagName": "v0.3.1-preview.12", "isDraft": false, "isPrerelease": true},
  {"tagName": "v0.4.1-preview.3", "isDraft": false, "isPrerelease": true},
  {"tagName": "v0.5.0", "isDraft": true, "isPrerelease": false},
  {"tagName": "v0.4.1-preview.2-fix", "isDraft": false, "isPrerelease": true},
  {"tagName": "v0.3.0", "isDraft": false, "isPrerelease": false}
]
EOF

mkdir "$dir/bin"
cat > "$dir/bin/gh" <<'EOF'
#!/usr/bin/env bash
case "$1 $2" in
"release list") cat "$DIR/releases.json" ;;
"release delete") echo "$*" >> "$DIR/deleted" ;;
*) echo "fake gh: unexpected $*" >&2; exit 1 ;;
esac
EOF
chmod +x "$dir/bin/gh"

failed=0
# check <name> <want deletes, one per line> <script args...>
check() {
  local name=$1 want=$2
  shift 2
  rm -f "$dir/deleted"
  touch "$dir/deleted"
  if ! DIR=$dir PATH="$dir/bin:$PATH" "$script" "$@" > "$dir/out" 2>&1; then
    echo "FAIL $name: exit status $?"
    sed 's/^/    /' "$dir/out"
    failed=1
    return
  fi
  if [ "$(cat "$dir/deleted")" != "$want" ]; then
    echo "FAIL $name: deleted"
    sed 's/^/    /' "$dir/deleted"
    echo "  want"
    printf '%s\n' "$want" | sed 's/^/    /'
    failed=1
  else
    echo "ok   $name"
  fi
}

# Keeps 10 (after 8, as a number), 8, 7, 6 and 5; 9 is not a prerelease,
# 0.3.1-preview.12 is an older version, the others are not previews, and
# the draft goes.
check "keeps the newest five" "release delete v0.4.1-preview.4 --cleanup-tag --yes
release delete v0.4.1-preview.3 --cleanup-tag --yes
release delete v0.3.1-preview.12 --cleanup-tag --yes
release delete v0.4.1-preview.11 --yes" 5
check "keeps seven" "release delete v0.3.1-preview.12 --cleanup-tag --yes
release delete v0.4.1-preview.11 --yes" 7
check "keeps more than there are" "release delete v0.4.1-preview.11 --yes" 20
check "keeps one" "release delete v0.4.1-preview.8 --cleanup-tag --yes
release delete v0.4.1-preview.7 --cleanup-tag --yes
release delete v0.4.1-preview.6 --cleanup-tag --yes
release delete v0.4.1-preview.5 --cleanup-tag --yes
release delete v0.4.1-preview.4 --cleanup-tag --yes
release delete v0.4.1-preview.3 --cleanup-tag --yes
release delete v0.3.1-preview.12 --cleanup-tag --yes
release delete v0.4.1-preview.11 --yes" 1
check "a dry run deletes nothing" "" --dry-run 5
if ! grep -q '^would run: gh release delete v0.4.1-preview.4 --cleanup-tag --yes$' "$dir/out"; then
  echo "FAIL the dry run's output"
  sed 's/^/    /' "$dir/out"
  failed=1
fi
for bad in "" 0 -1 x; do
  if DIR=$dir PATH="$dir/bin:$PATH" "$script" ${bad:+"$bad"} > /dev/null 2>&1; then
    echo "FAIL KEEP \"$bad\" is refused"
    failed=1
  fi
done

exit "$failed"
