#!/usr/bin/env bash
# The repository gate. Local runs and CI both run this script.
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

# Print one FAIL line on any non-zero exit. The PASS line is the last line of a clean run.
on_exit() {
  local status=$?
  if [ "$status" -ne 0 ]; then
    echo "FAIL: tools/check.sh (exit $status)"
  fi
}
trap on_exit EXIT

# Go: gofmt over every Go file outside scratch/, then vet and race tests.
echo "== gofmt"
mapfile -d '' -t go_files < <(git ls-files -z --cached --others --exclude-standard -- '*.go')
fmt_targets=()
for f in "${go_files[@]}"; do
  case "$f" in
    scratch/*) continue ;;
  esac
  if [ -f "$f" ]; then
    fmt_targets+=("$f")
  fi
done
if [ "${#fmt_targets[@]}" -gt 0 ]; then
  unformatted="$(gofmt -l "${fmt_targets[@]}")"
  if [ -n "$unformatted" ]; then
    echo "gofmt: these files need formatting (run gofmt -w):" >&2
    echo "$unformatted" >&2
    exit 1
  fi
fi

# go vet and go test exit 1 when ./... matches no packages, so skip them in that case.
# scratch/ is gitignored and holds the earlier draft, so its packages are excluded too.
# -e keeps listing when a package in scratch/ has an unresolvable import. Without it, go list
# aborts before the filter runs. Packages outside scratch/ still reach vet and test, where a
# load or compile error fails the gate.
echo "== go list"
all_pkgs="$(go list -e -f '{{.ImportPath}}' ./...)"
pkgs="$(printf '%s\n' "$all_pkgs" | grep -v '/scratch/' || true)"
if [ -z "$pkgs" ]; then
  echo "note: ./... matches no Go packages outside scratch/; skipping go vet and go test"
else
  mapfile -t pkg_list <<< "$pkgs"
  echo "== go vet"
  go vet "${pkg_list[@]}"
  echo "== go test -race"
  go test -race "${pkg_list[@]}"
fi

# Frontend: only when web/ exists. Runs each of lint, check and test that web/package.json defines.
if [ -f web/package.json ]; then
  echo "== web install"
  pnpm --dir web install --frozen-lockfile
  for script in lint check test; do
    if node -e 'const fs = require("fs"); const s = JSON.parse(fs.readFileSync("web/package.json", "utf8")).scripts || {}; process.exit(s[process.argv[1]] ? 0 : 1)' "$script"; then
      echo "== web $script"
      pnpm --dir web run "$script"
    fi
  done
else
  echo "note: web/ absent; skipping frontend checks"
fi

echo "PASS: tools/check.sh"
