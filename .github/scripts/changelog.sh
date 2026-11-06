#!/usr/bin/env bash
#
# Collects the changelog entries of all PRs merged since the last release into CHANGELOG.md lines.
#
# Usage: .github/scripts/changelog.sh [<since> [<until>]]
#
#   <since>  Git ref of the previous release (default: the latest tag reachable from <until>).
#   <until>  Git ref of the upcoming release (default: HEAD).
#
# Each PR's entry is taken from its description, starting at the first line that begins with
# "Changelog:" up to the next blank line, as enforced by .github/workflows/changelog.yml.
# PRs labeled "no-changelog" are left out. Entries are re-wrapped to 120 characters per line.
#
# PRs are found by asking GitHub which merged PRs each commit in <since>..<until> belongs to, so
# this works regardless of the merge strategy (merge commit, squash, rebase) and for release
# branches too. Commits are looked up in batches of 100 per GraphQL request.
#
# Requires git, plus gh (authenticated) for reading the PRs.

set -euo pipefail

until="${2:-HEAD}"
since="${1:-$(git describe --tags --abbrev=0 "$until")}"

echo "Collecting changelog entries for $since..$until" >&2

# For each commit, prints one line per merged PR it belongs to:
#   <merged at><TAB><PR number><TAB><status><TAB><text>
# with status "entry" and the changelog entry, or "missing" and the PR title if there's no entry.
# PRs labeled "no-changelog" are left out. Commits without any merged PR print:
#   -<TAB><commit><TAB>none
#
# The entry starts at the first line beginning with "Changelog:" and, like a Markdown paragraph,
# continues up to the next blank line. Its lines are joined with spaces.
jq_filter='
  .data.repository | to_entries[]
  | [.value.associatedPullRequests.nodes[]? | select(.mergedAt != null)] as $prs
  | if $prs == [] then "-\t\(.key[2:])\tnone"
    else
      $prs[] | select(all(.labels.nodes[]; .name != "no-changelog"))
      | ((.body | gsub("\r"; "") | split("\n")) as $lines
        | (first(range($lines | length) | select($lines[.] | startswith("Changelog:"))) // null) as $i
        | if $i == null then ""
          else
            $lines[$i + 1:] as $rest
            | ([$rest[] | test("^\\s*$")] | index(true) // ($rest | length)) as $n
            | [($lines[$i] | sub("^Changelog:"; ""))] + $rest[:$n]
            | join(" ") | gsub("\\s+"; " ") | sub("^ "; "") | sub(" $"; "")
          end) as $entry
      | if $entry != "" then "\(.mergedAt)\t\(.number)\tentry\t\($entry)"
        else "\(.mergedAt)\t\(.number)\tmissing\t\(.title)"
        end
    end
'

commits=($(git rev-list "$since..$until"))
results=""
for ((i = 0; i < ${#commits[@]}; i += 100)); do
  # One aliased field per commit, e.g. c_<sha>: object(oid: "<sha>") { ...prs }
  fields=""
  for commit in "${commits[@]:i:100}"; do
    fields+="c_$commit: object(oid: \"$commit\") { ...prs } "
  done

  results+="$(gh api graphql -F owner='{owner}' -F repo='{repo}' --jq "$jq_filter" -f query="
    query(\$owner: String!, \$repo: String!) {
      repository(owner: \$owner, name: \$repo) { $fields }
    }
    fragment prs on Commit {
      associatedPullRequests(first: 10) {
        nodes { number mergedAt title body labels(first: 100) { nodes { name } } }
      }
    }
  ")"$'\n'
done

# Wraps a list item to at most 120 characters per line, indenting continuation lines by two spaces.
# Words longer than that (e.g. URLs) are kept intact.
wrap() {
  awk -v width=120 '{
    n = split($0, words, " ")
    line = words[1]
    for (i = 2; i <= n; i++) {
      if (length(line) + 1 + length(words[i]) > width) { print line; line = "  " words[i] }
      else line = line " " words[i]
    }
    print line
  }' <<<"$1"
}

missing=0

# Each PR once (all its commits print the same line), ordered by merge time (oldest first).
# ISO 8601 timestamps sort lexically, and "-" (commits without a PR) sorts before them.
while IFS=$'\t' read -r _ id status text; do
  case "$status" in
    none)
      echo "Warning: commit $(git log -1 --format='%h "%s"' "$id") doesn't belong to any merged PR." >&2
      ;;
    entry)
      wrap "* $text #$id"
      ;;
    missing)
      echo "Warning: #$id has neither a changelog entry nor the 'no-changelog' label: $text" >&2
      wrap "* TODO (no changelog entry): $text #$id"
      missing=$((missing + 1))
      ;;
  esac
done < <(grep -v '^$' <<<"$results" | LC_ALL=C sort -u)

if [ "$missing" -gt 0 ]; then
  echo "$missing PR(s) without a changelog entry, see the TODO lines above." >&2
fi
