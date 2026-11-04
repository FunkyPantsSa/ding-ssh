#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
dist_assets="$repo_root/frontend/dist/assets"

# Vite 7 / Vitest 5 require Node >= 22.12; fail early instead of surfacing a jsdom ESM error.
node -e 'const [major, minor] = process.versions.node.split(".").map(Number); if (major < 22 || (major === 22 && minor < 12)) { console.error(`error: Node >= 22.12 required (current ${process.versions.node}); see .nvmrc`) ; process.exit(1) }'

echo "== frontend typecheck + build =="
(cd "$repo_root/frontend" && npm run build)

echo "== frontend unit and UI contract tests =="
(cd "$repo_root/frontend" && npm test)

echo "== go tests =="
(cd "$repo_root" && go test ./...)

echo "== UI 2.0 baseline budgets =="

mapfile_compat() {
  local target_name="$1"
  shift
  local value
  eval "$target_name=()"
  while IFS= read -r value; do
    eval "$target_name+=(\"\$value\")"
  done < <("$@")
}

mapfile_compat css_files find "$dist_assets" -maxdepth 1 -type f -name '*.css' -print
mapfile_compat font_files find "$dist_assets" -maxdepth 1 -type f \( -name '*.woff' -o -name '*.woff2' \) -print

if ((${#css_files[@]} == 0)); then
  echo "error: no built CSS files found in $dist_assets" >&2
  exit 1
fi

css_bytes=0
font_bytes=0
font_face_count=0
css_gzip_bytes=0

for file in "${css_files[@]}"; do
  bytes="$(wc -c < "$file" | tr -d ' ')"
  css_bytes=$((css_bytes + bytes))
  gzip_bytes="$(gzip -c "$file" | wc -c | tr -d ' ')"
  css_gzip_bytes=$((css_gzip_bytes + gzip_bytes))
  count="$(awk '{ total += gsub(/@font-face/, "") } END { print total + 0 }' "$file")"
  font_face_count=$((font_face_count + count))
done

for file in "${font_files[@]}"; do
  bytes="$(wc -c < "$file" | tr -d ' ')"
  font_bytes=$((font_bytes + bytes))
done

printf 'css files: %d\n' "${#css_files[@]}"
printf 'css bytes: %d\n' "$css_bytes"
printf 'css gzip bytes: %d\n' "$css_gzip_bytes"
printf 'font files: %d\n' "${#font_files[@]}"
printf 'font bytes: %d\n' "$font_bytes"
printf 'font-face: %d\n' "$font_face_count"

mapfile_compat js_files find "$dist_assets" -maxdepth 1 -type f -name 'index-*.js' -print
js_bytes=0
js_gzip_bytes=0
for file in "${js_files[@]}"; do
  js_bytes=$((js_bytes + $(wc -c < "$file" | tr -d ' ')))
  js_gzip_bytes=$((js_gzip_bytes + $(gzip -c "$file" | wc -c | tr -d ' ')))
done
dist_bytes="$(du -sk "$repo_root/frontend/dist" | awk '{print $1 * 1024}')"
printf 'js bytes: %d\n' "$js_bytes"
printf 'js gzip bytes: %d\n' "$js_gzip_bytes"
printf 'dist bytes: %d\n' "$dist_bytes"

((css_bytes <= 100000)) || echo "warning: CSS raw budget exceeded (target <= 100000)" >&2
((css_gzip_bytes <= 30000)) || echo "warning: CSS gzip budget exceeded (target <= 30000)" >&2
((js_bytes <= 900000)) || { echo "error: JS raw budget exceeded" >&2; exit 1; }
((js_gzip_bytes <= 270000)) || { echo "error: JS gzip budget exceeded" >&2; exit 1; }
((font_face_count <= 32)) || { echo "error: font-face budget exceeded" >&2; exit 1; }
((${#font_files[@]} <= 32)) || { echo "error: font file budget exceeded" >&2; exit 1; }
((dist_bytes <= 2621440)) || { echo "error: dist size budget exceeded" >&2; exit 1; }

echo "== UI 2.0 migration guards =="
legacy_pattern='--(ink|mist|signal|copper|ok-500|warn-500|danger-500|primary-ink)'
# Presentation files must consume semantic CSS variables. Literal colors belong
# only in src/theme (palette primitives/presets) or in TypeScript color helpers.
hardcoded_color_pattern='(#[0-9A-Fa-f]{3,8}\b|rgba?\([[:space:]]*[0-9])'
if command -v rg >/dev/null 2>&1; then
  legacy_refs="$(rg -n --glob '*.{vue,ts,css}' -- "$legacy_pattern" "$repo_root/frontend/src" || true)"
  fallback_refs="$(rg -n 'fallback\??:' "$repo_root/frontend/src/debug/ui.ts" || true)"
  hardcoded_color_refs="$(rg -n --glob '*.{vue,css}' --glob '!**/theme/**' --glob '!**/*.test.*' -- "$hardcoded_color_pattern" "$repo_root/frontend/src" || true)"
else
  legacy_refs="$(grep -rnE --include='*.vue' --include='*.ts' --include='*.css' -- "$legacy_pattern" "$repo_root/frontend/src" || true)"
  fallback_refs="$(grep -nE 'fallback\??:' "$repo_root/frontend/src/debug/ui.ts" || true)"
  hardcoded_color_refs="$(grep -rnE --include='*.vue' --include='*.css' --exclude='*.test.*' --exclude-dir='theme' -- "$hardcoded_color_pattern" "$repo_root/frontend/src" || true)"
fi
if [[ -n "$legacy_refs" ]]; then
  echo "error: legacy UI tokens remain outside the semantic token system:" >&2
  echo "$legacy_refs" >&2
  exit 1
fi
[[ ! -e "$repo_root/frontend/src/theme/aliases.css" ]] || { echo "error: aliases.css must not be reintroduced" >&2; exit 1; }
if [[ -n "$fallback_refs" ]]; then
  echo "error: ui.layout must not retain a class-selector fallback" >&2
  exit 1
fi
if [[ -n "$hardcoded_color_refs" ]]; then
  echo "error: hardcoded presentation colors must be defined as semantic tokens in frontend/src/theme:" >&2
  echo "$hardcoded_color_refs" >&2
  exit 1
fi
echo "migration guards: passed"
