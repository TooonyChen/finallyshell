#!/bin/sh
# Assembles the site into the folder given (default _site): the pages of
# site/, the README's screenshots and the app icon. Run from the
# repository root: sh site/build.sh
set -eu
out="${1:-_site}"
rm -rf "$out"
mkdir -p "$out"
cp -R site/. "$out/"
rm "$out/build.sh"
cp -R docs/screenshots "$out/screenshots"
cp resources/icon.png "$out/icon.png"
