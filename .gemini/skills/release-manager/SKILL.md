---
name: release-manager
description: Orchestrates the release process for new versions of EchonetGO. Use this skill when the user asks to release a new version, bump versions, or prepare a new release.
---

# Release Manager

This skill manages releases for EchonetGO, including local multi-architecture Docker builds and publishing to GitHub Container Registry (GHCR).

## MANDATORY DUE DILIGENCE
Before ANY push to upstream (dev or main), you MUST:
1. Run local build: `go build ./...`
2. Run all tests: `go test ./...`
3. If ANY step fails, you MUST fix it before proceeding.

## Branch Model

- **`dev`** — Active development. Uses slug `echonetgo_dev`, name `EchonetGO (Dev)`, version `X.Y.Z-dev.N`.
- **`main`** — Stable production. Uses slug `echonetgo`, name `EchonetGO`, version `X.Y.Z`.

## Prerequisites

Before starting the release process:

1. Check which branch you are on (`git branch --show-current`).
2. Read the current version from `addon_echonetgo/config.yaml` (the `version` field).
3. Review commits since the last version bump using `git log $(git describe --tags --abbrev=0 2>/dev/null || echo HEAD~10)..HEAD --oneline`.
4. Suggest a version bump based on the branch and changes (see below).
5. **Ensure the user has authorized Docker to GHCR:** Ask if they have run `docker login ghcr.io`.
6. Present the current version and your suggested new version to the user for confirmation before proceeding.

### Version Bump Rules

**On `dev` branch:**
- If current version is `X.Y.Z-dev.N`, suggest `X.Y.Z-dev.(N+1)`.
- If starting a new dev cycle after a production release, suggest `X.Y.Z-dev.1` where X.Y.Z is the next target version.

**On `main` branch (production release):**
- Production follows `0.9.x` versioning (increment patch from last release).
- Simply increment the patch number from the last production tag (e.g., `v0.9.41` → `v0.9.42`).

## Dev Bump Workflow (on `dev` branch)

1. **Verify:** Run `go build ./...` and `go test ./...`.
2. **Bump:** Bump `version` in `addon_echonetgo/config.yaml` (increment the `-dev.N` counter).
3. **Build & Push Images:** Run this command to publish to GHCR (dev builds only target aarch64 to save time):
   - `docker buildx build --platform linux/arm64 -t ghcr.io/styygeli/aarch64-echonetgo_dev:<version> --push -f addon_echonetgo/Dockerfile .`
4. **Commit:** Commit with message: `chore: bump dev to X.Y.Z-dev.N`
5. **Push:** Push to origin.

## Production Release Workflow (on `main` branch)

Follow these steps exactly in order:

### 1. Merge dev into main
- `git checkout main && git merge dev`
- Resolve any conflicts.

### 2. Verify
- Run `go build ./...` and `go test ./...`.

### 3. Transform for production
Update `addon_echonetgo/config.yaml`:
- `name: EchonetGO`
- `slug: echonetgo`
- `version: "X.Y.Z"` (strip `-dev.N` suffix)
- `image: "ghcr.io/styygeli/{arch}-echonetgo"`

### 4. Build & Push Production Images
- `docker buildx build --platform linux/amd64 -t ghcr.io/styygeli/amd64-echonetgo:<version> --push -f addon_echonetgo/Dockerfile .`
- `docker buildx build --platform linux/arm64 -t ghcr.io/styygeli/aarch64-echonetgo:<version> --push -f addon_echonetgo/Dockerfile .`

### 5. Update CHANGELOG.md
Update `addon_echonetgo/CHANGELOG.md`.
1. Use `git log` to review recent commits and draft a summary of changes.
2. Add the new version header with current date.

### 6. Prepare Commit and Tag
Present a summary of all modified files to the user.
**Stop and wait for user approval.**

Once approved:
- Stage the changes.
- Commit with message: `chore: release vX.Y.Z`
- Create git tag: `git tag vX.Y.Z`

### 7. Push and GitHub Release
- Push main and the tag to origin.
- Create a GitHub release using `gh release create`.

### 8. Cleanup Dev Images
Inform the user: "Release complete. Please manually delete stale `-dev` packages for this version in your GitHub Packages UI."

### 9. Advance dev branch
- `git checkout dev && git merge main`
- Bump version to next target: `X.Y.Z-dev.1`
- Restore dev config: `name: "EchonetGO (Dev)"`, `slug: echonetgo_dev`, `image: "ghcr.io/styygeli/{arch}-echonetgo_dev"`
- Commit: `chore: begin vX.Y.Z development cycle`
- Push dev.
