#!/usr/bin/env bash
# Moves the factory's base branch (dogfood) up to origin/main, but only when that cannot create a
# merge conflict for a PR the factory already has open. Every move of the base re-creates the
# conflict for open PRs that touch the same files and burns the task's attempts (tasks/lessons.md),
# so this script test-merges each open PR head into the new base first and, if any would conflict,
# leaves dogfood where it is and says which PRs are in the way. Run it again once they settle.
#
# Needs git and gh (signed in) on PATH, run from a clone of the repository. It never forces: dogfood
# must fast-forward to main. ONETT_SYNC_DRY_RUN=1 reports the decision and pushes nothing.
# Never run this from an agent that onett runs: it pushes with the owner's credentials.
set -euo pipefail

if [ -n "${ONETT_OUTBOX:-}" ]; then
  echo "sync-base.sh: refusing to run inside an onett agent run" >&2
  exit 1
fi
base=dogfood
repo=$(git -C "$(dirname "$0")" rev-parse --show-toplevel)
git="git -c core.hooksPath=/dev/null -C $repo"

$git fetch -q origin main "$base"
main=$($git rev-parse origin/main)
cur=$($git rev-parse "origin/$base")
if [ "$main" = "$cur" ]; then
  echo "sync-base: $base is already at main"
  exit 0
fi
if ! $git merge-base --is-ancestor "$cur" "$main"; then
  echo "sync-base: $base is not an ancestor of main; it will not fast-forward. Fix it by hand." >&2
  exit 1
fi

# Open PRs targeting the base. A failing gh must stop the script: an empty list from an error
# would look like "nothing in the way".
prs=$(gh pr list --base "$base" --state open --json number,headRefName --jq '.[] | "\(.number) \(.headRefName)"')
blocked=()
while read -r num ref; do
  [ -n "${num:-}" ] || continue
  $git fetch -q origin "$ref" || { echo "sync-base: cannot fetch PR #$num ($ref)" >&2; exit 1; }
  if ! $git merge-tree --write-tree "$main" FETCH_HEAD >/dev/null 2>&1; then
    blocked+=("#$num ($ref)")
  fi
done <<<"$prs"

if [ ${#blocked[@]} -gt 0 ]; then
  echo "sync-base: not moving $base; these open PRs would conflict with main:"
  printf '  %s\n' "${blocked[@]}"
  exit 2
fi
if [ -n "${ONETT_SYNC_DRY_RUN:-}" ]; then
  echo "sync-base: would fast-forward $base to ${main:0:12} (dry run)"
  exit 0
fi
$git push origin "$main:refs/heads/$base"
echo "sync-base: $base is now ${main:0:12}"
