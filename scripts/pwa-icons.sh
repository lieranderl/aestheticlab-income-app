#!/usr/bin/env bash
# Draws the installable app's PNG icons from the brand mark in
# static/icons/favicon.svg (the linen hummingbird on a sage square). Run it
# again only when the brand mark changes; the PNGs are committed.
#
# Needs rsvg-convert (brew install librsvg).
set -euo pipefail
cd "$(dirname "$0")/.."

src=static/icons/favicon.svg
out=static/icons
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

# The hummingbird, without the rounded square around it.
bird=$(grep '<path' "$src")

# Full-bleed square: phones round or mask the corners themselves.
square() { # $1 = how much of the side the bird takes (1 = as in the favicon)
	cat <<SVG
<svg xmlns="http://www.w3.org/2000/svg" viewBox="345 165 254 254">
  <rect x="345" y="165" width="254" height="254" fill="#8b9687"/>
  <g transform="translate(472 292) scale($1) translate(-472 -292)">$bird</g>
</svg>
SVG
}
square 1 >"$tmp/any.svg"
square 0.62 >"$tmp/maskable.svg" # tail tip inside the maskable safe zone (the central 80% circle)

rsvg-convert -w 192 -h 192 "$tmp/any.svg" -o "$out/icon-192.png"
rsvg-convert -w 512 -h 512 "$tmp/any.svg" -o "$out/icon-512.png"
rsvg-convert -w 512 -h 512 "$tmp/maskable.svg" -o "$out/icon-maskable-512.png"
rsvg-convert -w 180 -h 180 "$tmp/any.svg" -o "$out/apple-touch-icon.png"
echo "Wrote $out/icon-192.png, icon-512.png, icon-maskable-512.png, apple-touch-icon.png"
