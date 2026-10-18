#!/usr/bin/env bash
# release.sh — cut a GitHub release (and its git tag) for this Go module.
#
# Go resolves module versions from semver git tags (vX.Y.Z), so the tag is
# what `go get github.com/dirty-go/foundation@latest` picks up. Tags are
# effectively immutable once proxy.golang.org has seen them: never move or
# re-create a tag; fix forward with a new version (and `retract` in go.mod).
#
# Usage:
#   scripts/release.sh [options] <patch|minor|major|vX.Y.Z[-pre]>
#
# Options:
#   -n, --dry-run        Run every check and print what would happen; push nothing.
#   -t, --target <rev>   Commit to tag (default: HEAD). Must be on <remote>/<branch>.
#   -y, --yes            Don't ask for confirmation.
#       --skip-tests     Skip go build/vet/test at the target (not recommended).
#       --no-warm        Don't request the version from proxy.golang.org afterwards.
#       --remote <name>  Git remote (default: origin).
#       --branch <name>  Release branch (default: main).
#   -h, --help           Show this help.
#
# Release notes come from the matching "## [vX.Y.Z]" section of CHANGELOG.md,
# so move [Unreleased] entries into a version section (via a PR) first.
#
# Examples:
#   scripts/release.sh --dry-run minor
#   scripts/release.sh v0.2.0
#   scripts/release.sh --target a5da92e v0.1.0

set -euo pipefail

remote=origin
branch=main
target=HEAD
dry_run=false
assume_yes=false
skip_tests=false
warm=true
spec=""

die()  { printf 'release: error: %s\n' "$*" >&2; exit 1; }
info() { printf '==> %s\n' "$*"; }
run()  {
	if $dry_run; then
		printf '[dry-run] %s\n' "$*"
	else
		"$@"
	fi
}
usage() { sed -n '2,/^$/p' "$0" | sed 's/^# \{0,1\}//'; }

while [ $# -gt 0 ]; do
	case $1 in
	-n | --dry-run) dry_run=true ;;
	-t | --target) [ $# -ge 2 ] || die "$1 needs a value"; target=$2; shift ;;
	-y | --yes) assume_yes=true ;;
	--skip-tests) skip_tests=true ;;
	--no-warm) warm=false ;;
	--remote) [ $# -ge 2 ] || die "$1 needs a value"; remote=$2; shift ;;
	--branch) [ $# -ge 2 ] || die "$1 needs a value"; branch=$2; shift ;;
	-h | --help) usage; exit 0 ;;
	-*) die "unknown option: $1 (see --help)" ;;
	*) [ -z "$spec" ] || die "only one version argument allowed"; spec=$1 ;;
	esac
	shift
done
[ -n "$spec" ] || { usage; exit 2; }

for cmd in git gh go awk; do
	command -v "$cmd" >/dev/null 2>&1 || die "$cmd is required"
done

root=$(git rev-parse --show-toplevel 2>/dev/null) || die "not inside a git repository"
cd "$root"
[ -f go.mod ] || die "no go.mod at repo root"
module=$(awk '$1 == "module" { print $2; exit }' go.mod)
[ -n "$module" ] || die "cannot read module path from go.mod"

gh auth status >/dev/null 2>&1 || die "gh is not authenticated (run: gh auth login)"

# --- Repository state ------------------------------------------------------

info "Fetching $remote (branch $branch, tags)"
git fetch --quiet --tags "$remote" "$branch"

[ -z "$(git status --porcelain)" ] || die "working tree is not clean"

current=$(git symbolic-ref --quiet --short HEAD || true)
[ "$current" = "$branch" ] || die "not on $branch (on '${current:-detached HEAD}')"

local_head=$(git rev-parse HEAD)
remote_head=$(git rev-parse "$remote/$branch")
[ "$local_head" = "$remote_head" ] ||
	die "local $branch ($(git rev-parse --short HEAD)) differs from $remote/$branch ($(git rev-parse --short "$remote/$branch")); pull or push first"

sha=$(git rev-parse --verify --quiet "$target^{commit}") || die "unknown target: $target"
git merge-base --is-ancestor "$sha" "$remote/$branch" ||
	die "target $target is not on $remote/$branch"

# --- Version ---------------------------------------------------------------

semver_re='^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?$'
release_re='^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$'

# Highest release (non-prerelease) tag, or empty.
latest=$(git tag -l 'v*' | grep -E "$release_re" | sed 's/^v//' |
	sort -t. -k1,1n -k2,2n -k3,3n | tail -n1 | sed 's/^./v&/' || true)
if [ -n "$latest" ]; then
	IFS=. read -r lmaj lmin lpat <<EOF
${latest#v}
EOF
else
	lmaj=0 lmin=0 lpat=0
fi

case $spec in
patch) version="v$lmaj.$lmin.$((lpat + 1))" ;;
minor) version="v$lmaj.$((lmin + 1)).0" ;;
major) version="v$((lmaj + 1)).0.0" ;;
v*) version=$spec ;;
*) die "version must be patch, minor, major or vX.Y.Z (got '$spec')" ;;
esac
[ -z "$latest" ] && [ "$spec" = patch ] && version=v0.0.1

printf '%s\n' "$version" | grep -Eq "$semver_re" ||
	die "'$version' is not a valid Go module version (vMAJOR.MINOR.PATCH[-pre])"

prerelease=false
case $version in *-*) prerelease=true ;; esac

core=${version#v}
core=${core%%-*}
IFS=. read -r maj min pat <<EOF
$core
EOF

# The MAJOR.MINOR.PATCH core must be strictly greater than the latest release
# (a prerelease of an already-released core would sort below it).
newer=false
if [ -z "$latest" ]; then
	newer=true
elif [ "$maj" -gt "$lmaj" ]; then
	newer=true
elif [ "$maj" -eq "$lmaj" ] && [ "$min" -gt "$lmin" ]; then
	newer=true
elif [ "$maj" -eq "$lmaj" ] && [ "$min" -eq "$lmin" ] && [ "$pat" -gt "$lpat" ]; then
	newer=true
fi
$newer || die "$version is not newer than the latest release $latest"

# v2+ needs the /vN suffix in the module path, or `go get` can't resolve it.
if [ "$maj" -ge 2 ]; then
	case $module in
	*/v"$maj") ;;
	*) die "$version needs module path '$module/v$maj' in go.mod (Go semantic import versioning)" ;;
	esac
fi

if git rev-parse --verify --quiet "refs/tags/$version" >/dev/null; then
	die "tag $version already exists locally"
fi
if [ -n "$(git ls-remote --tags "$remote" "refs/tags/$version")" ]; then
	die "tag $version already exists on $remote"
fi
if gh release view "$version" >/dev/null 2>&1; then
	die "GitHub release $version already exists"
fi

# --- Release notes from CHANGELOG.md -----------------------------------------

[ -f CHANGELOG.md ] || die "CHANGELOG.md not found"
tmpdir=$(mktemp -d)
worktree=""
cleanup() {
	if [ -n "$worktree" ]; then
		git worktree remove --force "$worktree" >/dev/null 2>&1 || true
	fi
	rm -rf "$tmpdir"
}
trap cleanup EXIT

notes=$tmpdir/notes.md
# The section body: after the "## [vX.Y.Z]" header, up to the next version
# header or the link-reference block. $(...) also strips trailing newlines.
section=$(awk -v hdr="## [$version]" '
	index($0, hdr) == 1 { found = 1; next }
	found && (/^## \[/ || /^\[[^]]+\]: /) { exit }
	found { print }
' CHANGELOG.md | sed -e '/./,$!d')
printf '%s\n' "$section" | grep -q '[^[:space:]]' ||
	die "no '## [$version]' section with content in CHANGELOG.md; move [Unreleased] entries into it first"
printf '%s\n' "$section" >"$notes"

# --- Build, vet and test the exact commit being tagged -----------------------

if $skip_tests; then
	info "Skipping tests (--skip-tests)"
else
	info "Testing $version at $(git rev-parse --short "$sha") in a temporary worktree"
	worktree=$tmpdir/worktree
	git worktree add --quiet --detach "$worktree" "$sha"
	(cd "$worktree" && go build ./... && go vet ./... && go test ./...) ||
		die "build/vet/test failed at $(git rev-parse --short "$sha")"
fi

# --- Confirm and release -----------------------------------------------------

kind=""
$prerelease && kind=" (prerelease)"
cat <<EOF

  module:     $module
  version:    $version$kind
  latest:     ${latest:-none}
  target:     $(git log -1 --format='%h %s' "$sha")
  remote:     $remote
  go get:     go get $module@$version
EOF
printf '\n--- release notes ---\n'
cat "$notes"
printf -- '---------------------\n\n'

if ! $dry_run && ! $assume_yes; then
	printf 'Create release %s? Tags are permanent for Go. [y/N] ' "$version"
	read -r answer
	case $answer in y | Y | yes | YES) ;; *) die "aborted" ;; esac
fi

gh_args=(release create "$version" --target "$sha" --title "$version" --notes-file "$notes")
$prerelease && gh_args+=(--prerelease)
run gh "${gh_args[@]}"
run git fetch --quiet --tags "$remote"

# --- Make the version visible to `go get` right away -------------------------

if $warm && ! $prerelease; then
	# Module paths are case-encoded in proxy URLs: uppercase -> !lowercase.
	escaped=$(printf '%s' "$module" | sed -e 's/\([A-Z]\)/!\1/g' | tr '[:upper:]' '[:lower:]')
	url="https://proxy.golang.org/$escaped/@v/$version.info"
	if $dry_run; then
		printf '[dry-run] curl -fsS %s\n' "$url"
	elif command -v curl >/dev/null 2>&1; then
		info "Requesting $version from proxy.golang.org"
		ok=false
		for _ in 1 2 3 4 5; do
			if curl -fsS "$url" >/dev/null 2>&1; then ok=true; break; fi
			sleep 3
		done
		if $ok; then
			info "proxy.golang.org has $version"
		else
			printf 'release: warning: proxy.golang.org does not list %s yet; it will after the first go get\n' "$version" >&2
		fi
	fi
fi

if $dry_run; then
	info "Dry run complete; nothing was pushed"
else
	info "Released $version: go get $module@$version"
fi
