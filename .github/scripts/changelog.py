#!/usr/bin/env python3
"""Collects the changelog entries of all PRs merged since the last release into CHANGELOG.md lines.

Usage: .github/scripts/changelog.py [<since> [<until>]]

  <since>  Git ref of the previous release (default: the latest tag reachable from <until>).
  <until>  Git ref of the upcoming release (default: HEAD).

Each PR's entry is taken from its description, starting at the first line that begins with
"Changelog:" up to the next blank line, as enforced by .github/workflows/changelog.yml. PRs labeled
"no-changelog" are left out. Entries are ordered by merge time and wrapped to 120 characters per line.

PRs are found by asking GitHub which merged PRs each commit in <since>..<until> belongs to, so this
works regardless of the merge strategy (merge commit, squash, rebase) and for release branches too.
Commits are looked up in batches of 100 per GraphQL request.

Requires Python 3.12 or newer and git, plus gh (authenticated) for reading the PRs.
"""

import argparse
import itertools
import json
import subprocess
import sys
import textwrap

BATCH_SIZE = 100
WIDTH = 120
SKIP_LABEL = "no-changelog"
PREFIX = "Changelog:"

QUERY = """
query($owner: String!, $repo: String!) {
  repository(owner: $owner, name: $repo) { %s }
}
fragment prs on Commit {
  associatedPullRequests(first: 10) {
    nodes { number mergedAt title body labels(first: 100) { nodes { name } } }
  }
}
"""


def run(*args: str) -> str:
    """Runs the given command and returns its stdout, exiting with an error if it fails."""

    try:
        return subprocess.run(args, check=True, stdout=subprocess.PIPE, text=True).stdout
    except FileNotFoundError:
        sys.exit(f"Error: {args[0]} not found, please install it.")
    except subprocess.CalledProcessError as e:
        # The command has already printed its error to stderr.
        sys.exit(f"Error: {' '.join(args[:2])} failed with exit code {e.returncode}.")


def warn(message: str) -> None:
    print(message, file=sys.stderr)


def extract(body: str) -> str | None:
    """Returns the changelog entry of the given PR description, or None if there is none.

    The entry starts at the first line beginning with "Changelog:" and, like a Markdown paragraph,
    continues up to the next blank line. Its lines are joined into a single line.
    """

    # splitlines() also handles the CRLF line endings of descriptions edited on GitHub.
    lines = iter(body.splitlines())
    for line in lines:
        if line.startswith(PREFIX):
            paragraph = [line.removeprefix(PREFIX)]
            break
    else:
        return None

    for line in lines:
        if not line.strip():
            break
        paragraph.append(line)

    # Collapse all whitespace, including the line breaks, into single spaces.
    return " ".join(" ".join(paragraph).split()) or None


def merged_prs(commits: dict[str, str]) -> dict[int, dict]:
    """Returns the merged PRs the given commits belong to, by number, warning about commits without any.

    The commits map each full hash to a short description used in the warnings.
    """

    prs = {}
    for batch in itertools.batched(commits.keys(), BATCH_SIZE):
        # One aliased field per commit, e.g. c_<sha>: object(oid: "<sha>") { ...prs }
        fields = " ".join(f'c_{c}: object(oid: "{c}") {{ ...prs }}' for c in batch)
        result = json.loads(run(
            "gh", "api", "graphql", "-F", "owner={owner}", "-F", "repo={repo}", "-f", f"query={QUERY % fields}",
        ))

        for alias, commit in result["data"]["repository"].items():
            # A commit is null if GitHub doesn't know it, e.g. because it hasn't been pushed.
            nodes = (commit or {}).get("associatedPullRequests", {}).get("nodes", [])
            merged = [pr for pr in nodes if pr["mergedAt"] is not None]
            if not merged:
                warn(f"Warning: commit {commits[alias.removeprefix('c_')]} doesn't belong to any merged PR.")
            for pr in merged:
                prs[pr["number"]] = pr

    return prs


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    parser.add_argument("since", nargs="?", help="Git ref of the previous release (default: latest tag)")
    parser.add_argument("until", nargs="?", default="HEAD", help="Git ref of the upcoming release (default: HEAD)")
    args = parser.parse_args()

    since = args.since or run("git", "describe", "--tags", "--abbrev=0", args.until).strip()
    warn(f"Collecting changelog entries for {since}..{args.until}")

    # Full hash -> short hash and subject, for the warnings about commits without any merged PR.
    log = run("git", "log", "--format=%H %h \"%s\"", f"{since}..{args.until}")
    commits = dict(line.split(" ", 1) for line in log.splitlines())
    if not commits:
        hint = "" if args.since else (
            f" {args.until} is tagged as {since} already, so pass the previous release explicitly,"
            f" e.g.: {sys.argv[0]} <previous release> {args.until}"
        )
        sys.exit(f"Error: there are no commits in {since}..{args.until}.{hint}")

    prs = merged_prs(commits)

    # Collected first and printed after all warnings, so that they don't get mixed up in the terminal.
    entries = []
    missing = 0
    # Ordered by merge time (oldest first). ISO 8601 timestamps sort lexically.
    for pr in sorted(prs.values(), key=lambda pr: pr["mergedAt"]):
        if any(label["name"] == SKIP_LABEL for label in pr["labels"]["nodes"]):
            continue

        entry = extract(pr["body"] or "")
        if entry is None:
            warn(f"Warning: #{pr['number']} has neither a changelog entry nor the '{SKIP_LABEL}' label: {pr['title']}")
            entry = f"TODO (no changelog entry): {pr['title']}"
            missing += 1

        # Words longer than the width (e.g. URLs) are kept intact.
        entries.append(textwrap.fill(
            f"{entry} #{pr['number']}", width=WIDTH, initial_indent="* ", subsequent_indent="  ",
            break_long_words=False, break_on_hyphens=False,
        ))

    if missing:
        warn(f"{missing} PR(s) without a changelog entry, see the TODO lines below.")

    for entry in entries:
        print(entry)


if __name__ == "__main__":
    main()
