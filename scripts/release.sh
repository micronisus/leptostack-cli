#!/usr/bin/env bash
# Portable release entrypoint shared by GitHub Actions and Forgejo Actions.
#
# Builds all platform artifacts with GoReleaser (without publishing), then
# publishes the release and assets to whichever forge is running the job:
#   - GitHub  (github.com)   via the `gh` CLI
#   - Forgejo/Gitea          via the forge REST API with curl + jq
#
# Required environment for publishing:
#   GitHub:  GH_TOKEN (or GITHUB_TOKEN), provided by the workflow.
#   Forgejo: FORGEJO_TOKEN (or GITEA_TOKEN/GITHUB_TOKEN) plus GITHUB_SERVER_URL
#            (set by the runner) or FORGEJO_URL/GITEA_URL.
# Optional:
#   RELEASE_VERSION      override the tag-derived version (without leading "v").
#   GORELEASER_VERSION   GoReleaser version to install (default pinned below).
set -euo pipefail

cd "$(dirname "$0")/.."

GORELEASER_VERSION="${GORELEASER_VERSION:-v2.18.3}"

die() { echo "Error: $*" >&2; exit 1; }
group() { printf '::group::%s\n' "$1"; }
endgroup() { printf '::endgroup::\n'; }

# --- determine version -------------------------------------------------------

VERSION="${RELEASE_VERSION:-}"
if [ -z "$VERSION" ]; then
  for ref in "${GITHUB_REF_NAME:-}" "${FORGEJO_REF_NAME:-}" "${GITEA_REF_NAME:-}" "${GITHUB_REF:-}" "${FORGEJO_REF:-}"; do
    case "$ref" in
      v[0-9]*) VERSION="${ref#v}"; break ;;
      refs/tags/v*) VERSION="${ref#refs/tags/v}"; break ;;
    esac
  done
fi
[ -n "$VERSION" ] || die "could not determine the release version (set RELEASE_VERSION)"
TAG="v$VERSION"
echo "Releasing $TAG"

[ -d .git ] || die "release must run from a git checkout"

# --- build artifacts ---------------------------------------------------------

group "Install GoReleaser"
if ! command -v goreleaser >/dev/null 2>&1; then
  # Installed from the Go module proxy so builds work without GitHub access.
  echo "Installing goreleaser ${GORELEASER_VERSION} via 'go install'..."
  go install "github.com/goreleaser/goreleaser/v2@${GORELEASER_VERSION}"
fi
GOPATH_BIN="$(go env GOPATH)/bin"
case ":$PATH:" in
  *":$GOPATH_BIN:"*) ;;
  *) export PATH="$GOPATH_BIN:$PATH" ;;
esac
command -v goreleaser >/dev/null 2>&1 || die "goreleaser is not available"
goreleaser --version
endgroup

group "Build release artifacts"
export GORELEASER_CURRENT_TAG="$TAG"
goreleaser release --clean --skip=publish
endgroup

# --- collect assets ----------------------------------------------------------

ASSETS=()
for f in dist/leptostack_*.tar.gz dist/checksums.txt; do
  [ -e "$f" ] && ASSETS+=("$f")
done
[ "${#ASSETS[@]}" -gt 0 ] || die "no release artifacts were produced"
echo "Release assets:"
printf '  %s\n' "${ASSETS[@]}"

# --- publish -----------------------------------------------------------------

SERVER="${FORGEJO_URL:-${GITEA_URL:-${GITHUB_SERVER_URL:-}}}"

publish_github() {
  command -v gh >/dev/null 2>&1 || die "the gh CLI is required for GitHub releases"
  if gh release view "$TAG" >/dev/null 2>&1; then
    echo "Uploading assets to existing release $TAG..."
    gh release upload "$TAG" "${ASSETS[@]}" --clobber
  else
    echo "Creating release $TAG..."
    gh release create "$TAG" --title "$TAG" --generate-notes "${ASSETS[@]}"
  fi
}

publish_gitea() {
  command -v curl >/dev/null 2>&1 || die "curl is required for Forgejo/Gitea releases"
  command -v jq >/dev/null 2>&1 || die "jq is required for Forgejo/Gitea releases"

  local server token repo owner name api id f
  server="$SERVER"
  if [ -z "$server" ]; then
    server="$(git remote get-url origin 2>/dev/null | sed -E 's#^(https?://[^/]+)/.*#\1#' || true)"
  fi
  [ -n "$server" ] || die "cannot determine the forge server URL (set FORGEJO_URL/GITEA_URL)"
  server="${server%/}"

  token="${FORGEJO_TOKEN:-${GITEA_TOKEN:-${GITHUB_TOKEN:-${GH_TOKEN:-}}}}"
  [ -n "$token" ] || die "no forge API token (set FORGEJO_TOKEN/GITEA_TOKEN/GITHUB_TOKEN)"

  repo="${FORGEJO_REPOSITORY:-${GITEA_REPOSITORY:-${GITHUB_REPOSITORY:-}}}"
  if [ -z "$repo" ]; then
    repo="$(git remote get-url origin | sed -E 's#.*[:/]([^/]+/[^/]+)$#\1#' | sed 's/\.git$//')"
  fi
  owner="${repo%%/*}"
  name="${repo##*/}"
  api="$server/api/v1/repos/$owner/$name"
  echo "Publishing to ${server} (${owner}/${name})"

  id="$(curl -fsS -H "Authorization: token $token" "$api/releases/tags/$TAG" 2>/dev/null | jq -r '.id // empty' || true)"
  if [ -z "$id" ]; then
    echo "Creating release $TAG..."
    id="$(curl -fsS -X POST \
      -H "Authorization: token $token" \
      -H "Content-Type: application/json" \
      -d "{\"tag_name\":\"$TAG\",\"name\":\"$TAG\",\"draft\":false,\"prerelease\":false}" \
      "$api/releases" | jq -r '.id')"
  fi
  [ -n "$id" ] && [ "$id" != "null" ] || die "could not create or find release $TAG"

  for f in "${ASSETS[@]}"; do
    echo "Uploading $(basename "$f")..."
    curl -fsS -X POST \
      -H "Authorization: token $token" \
      -F "attachment=@$f" \
      "$api/releases/$id/assets?name=$(basename "$f")" >/dev/null
  done
}

if [ "${GITHUB_ACTIONS:-}" = "true" ] && printf '%s' "$SERVER" | grep -q 'github\.com'; then
  publish_github
else
  publish_gitea
fi

# --- container image ---------------------------------------------------------

IMAGE_REPOSITORY="${IMAGE_REPOSITORY:-ghcr.io/leptostack/leptostack-cli}"
PUBLISH_IMAGE="${PUBLISH_IMAGE:-auto}"

container_runtime() {
  if command -v docker >/dev/null 2>&1; then echo docker; return 0; fi
  if command -v podman >/dev/null 2>&1; then echo podman; return 0; fi
  return 1
}

is_github_forge() { printf '%s' "${SERVER:-}" | grep -q 'github\.com'; }

publish_image() {
  case "$PUBLISH_IMAGE" in
    0|false|no)
      echo "Skipping container image publish (PUBLISH_IMAGE=$PUBLISH_IMAGE)."
      return 0 ;;
    1|true|yes|auto) ;;
    *) die "invalid PUBLISH_IMAGE=$PUBLISH_IMAGE (use auto|1|0)" ;;
  esac
  # In auto mode publish only on github.com (the natural publisher for ghcr.io);
  # other forges opt in with PUBLISH_IMAGE=1 and registry credentials.
  if [ "$PUBLISH_IMAGE" = "auto" ] && ! is_github_forge; then
    echo "Skipping container image publish (auto: github.com only; set PUBLISH_IMAGE=1 to force)."
    return 0
  fi
  local rt
  if ! rt="$(container_runtime)"; then
    echo "Warning: no docker/podman available; skipping the container image." >&2
    return 0
  fi

  group "Build container image"
  "$rt" build \
    --build-arg "VERSION=$VERSION" \
    --build-arg "COMMIT=${GITHUB_SHA:-$(git rev-parse HEAD)}" \
    --build-arg "DATE=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    -t "$IMAGE_REPOSITORY:$VERSION" \
    -t "$IMAGE_REPOSITORY:latest" \
    .
  endgroup

  group "Publish container image"
  local registry user password
  registry="${IMAGE_REPOSITORY%%/*}"
  user="${IMAGE_REGISTRY_USER:-${GITHUB_ACTOR:-token}}"
  password="${IMAGE_REGISTRY_PASSWORD:-${GH_TOKEN:-${GITHUB_TOKEN:-}}}"
  if [ -n "$password" ]; then
    printf '%s' "$password" | "$rt" login "$registry" -u "$user" --password-stdin
  else
    echo "No registry credentials found; assuming $rt is already logged in to $registry."
  fi
  "$rt" push "$IMAGE_REPOSITORY:$VERSION"
  "$rt" push "$IMAGE_REPOSITORY:latest"
  endgroup
}

publish_image

echo "Released $TAG."
