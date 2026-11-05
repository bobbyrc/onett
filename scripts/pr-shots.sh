#!/usr/bin/env bash
# Publish the chat screenshots from `npm run shots:chat` and print a Markdown table for a PR.
#
#   cd web && npm run shots:chat && cd ..
#   scripts/pr-shots.sh before     # on the base commit
#   scripts/pr-shots.sh after      # on the PR head
#
# The PNGs in web/e2e-results/chat-shots are committed to a throwaway branch,
# pr-assets/<current-branch>/<label> (an orphan branch with no history in common with the code),
# and pushed to origin. The table links each image by commit SHA on raw.githubusercontent.com, so
# the links never change. The current branch and working tree are not touched: the commit is made
# in a temporary git worktree that is removed on exit. Never merge a pr-assets branch.
set -euo pipefail

label="${1:-}"
if [[ -z "$label" || "$label" == -* || ! "$label" =~ ^[A-Za-z0-9._-]+$ ]]; then
  echo "usage: scripts/pr-shots.sh <label>   (label: letters, digits, . _ -; e.g. before or after)" >&2
  exit 2
fi

root="$(git rev-parse --show-toplevel)"
src="$root/web/e2e-results/chat-shots"
shopt -s nullglob
pngs=("$src"/*.png)
if ((${#pngs[@]} == 0)); then
  echo "no PNGs in web/e2e-results/chat-shots: run 'npm run shots:chat' in web/ first" >&2
  exit 1
fi

branch="$(git -C "$root" rev-parse --abbrev-ref HEAD)"
if [[ "$branch" == HEAD ]]; then
  echo "detached HEAD: check out the branch you are taking shots for" >&2
  exit 1
fi
assets="pr-assets/$branch/$label"
# owner/repo from the origin URL (https or ssh form).
slug="$(git -C "$root" remote get-url origin | sed -E 's#^(git@[^:]+:|https?://[^/]+/)##; s#\.git$##')"

# Every git call here disables hooks, like onett's own (internal/workspace).
g() { git -c core.hooksPath=/dev/null "$@"; }

tmp="$(mktemp -d)"
cleanup() {
  g -C "$root" worktree remove --force "$tmp" >/dev/null 2>&1 || rm -rf "$tmp"
  g -C "$root" worktree prune
  g -C "$root" branch -D "$assets" >/dev/null 2>&1 || true
}
trap cleanup EXIT

# Start from the remote assets branch when it exists, otherwise from an empty orphan.
if g -C "$root" fetch --quiet origin "refs/heads/$assets" 2>/dev/null; then
  g -C "$root" worktree add --quiet -B "$assets" "$tmp" FETCH_HEAD
else
  g -C "$root" worktree add --quiet --detach "$tmp"
  g -C "$tmp" checkout --quiet --orphan "$assets"
  g -C "$tmp" rm -rfq . 2>/dev/null || true
fi

cp "${pngs[@]}" "$tmp/"
g -C "$tmp" add -A
if ! g -C "$tmp" diff --cached --quiet; then
  g -C "$tmp" commit --quiet -m "Screenshots: $label for $branch"
fi
sha="$(g -C "$tmp" rev-parse HEAD)"
g -C "$tmp" push --quiet origin "HEAD:refs/heads/$assets"

base="https://raw.githubusercontent.com/$slug/$sha"
echo "<!-- $label: $assets @ $sha -->"
echo "| scene | desktop light | desktop dark | iPhone light | iPhone dark |"
echo "|---|---|---|---|---|"
scenes="$(cd "$src" && ls ./*.png | sed -E 's#^\./##; s#-(desktop|iphone)-(light|dark)\.png$##' | sort -u)"
while IFS= read -r scene; do
  [[ -n "$scene" ]] || continue
  row="| $scene |"
  for v in desktop-light desktop-dark iphone-light iphone-dark; do
    f="$scene-$v.png"
    if [[ -f "$src/$f" ]]; then row+=" ![$scene $v]($base/$f) |"; else row+=" (missing) |"; fi
  done
  echo "$row"
done <<<"$scenes"
