# kt-dat Sync Design

## Goal

Replace the fragile Daed GraphQL routing mutation with a GitHub-backed sync that commits the current sing-box `route.rules[*].ip_cidr` set to `kt.txt` in `Van426326/kt-dat`.

The `kt-dat` repository remains responsible for packaging the text file into a dat release through its existing GitHub Actions workflow.

## Context

The current "同步到 Daed" flow extracts IP CIDR values from the saved sing-box config and mutates the selected Daed routing through GraphQL. This is brittle because it depends on Daed's routing text shape and GraphQL response schema.

The new flow should treat `kt-dat` as the synchronization target. `kt-proxy` only produces the canonical `kt.txt` content and commits it to GitHub. Daed then consumes the dat release produced by `kt-dat`.

## User-Facing Behavior

- The existing button should become "同步到 kt-dat".
- The frontend must keep the current unsaved-change guard: if the UI has unsaved edits, syncing is blocked and the user is told to save first.
- On success, the page should show the number of CIDR entries and, when available, the GitHub commit URL.
- If GitHub configuration is missing, the page should name the missing environment variables.

## Source Data

- Sync source is the saved sing-box config loaded through the existing config manager.
- Only `route.rules[*].ip_cidr` is included.
- `ip_cidr` may be either a string or an array of strings.
- Empty values are ignored.
- Duplicate values are removed while preserving first-seen order.
- The generated `kt.txt` content is one CIDR per line and ends with a trailing newline when at least one CIDR exists.

## GitHub Target

Default target:

- Repository: `Van426326/kt-dat`
- Branch: `main`
- Path: `kt.txt`

Runtime configuration:

- `KTDAT_REPO`, default `Van426326/kt-dat`
- `KTDAT_BRANCH`, default `main`
- `KTDAT_PATH`, default `kt.txt`
- `KTDAT_TOKEN`, required

`KTDAT_TOKEN` should be a GitHub fine-grained personal access token scoped only to `Van426326/kt-dat` with repository contents read/write permission.

## GitHub API Flow

Use GitHub Contents API instead of local git:

1. `GET /repos/{owner}/{repo}/contents/{path}?ref={branch}`
2. If the file exists, decode its base64 content and compare it to the generated content.
3. If content is identical, return success with `changed=false` and do not commit.
4. If content differs, `PUT /repos/{owner}/{repo}/contents/{path}` with:
   - `message`: `chore: update kt cidr list`
   - `content`: generated content base64
   - `branch`: configured branch
   - `sha`: existing file sha when updating an existing file
5. Return `changed=true`, CIDR count, commit SHA, and commit URL from GitHub's response.

The implementation should also support creating `kt.txt` if it does not exist. A 404 from the GET step means "create file", not a hard failure.

## Backend Shape

Introduce a new package:

- `internal/ktdatsync`

Responsibilities:

- Extract CIDR values from sing-box config.
- Render `kt.txt`.
- Validate runtime config and report missing env vars.
- Call GitHub Contents API.
- Return a sync result suitable for JSON responses.

The old `internal/daedsync` package and Daed GraphQL wiring should be removed or no longer used by the server. The frontend and tests must move to the new endpoint:

- `POST /api/ktdat/sync`

## Error Handling

- Missing `KTDAT_TOKEN` returns HTTP 400.
- Invalid `KTDAT_REPO` format returns HTTP 400.
- GitHub 401/403 returns HTTP 502 with a concise GitHub error.
- GitHub 409 returns HTTP 409 and tells the user the file changed remotely and they should retry.
- Other GitHub non-2xx responses return HTTP 502.
- Loading or parsing sing-box config errors keep existing behavior and return HTTP 500.

## Deployment

The Ubuntu install script should prompt for:

- `KTDAT_REPO`, default `Van426326/kt-dat`
- `KTDAT_BRANCH`, default `main`
- `KTDAT_PATH`, default `kt.txt`
- `KTDAT_TOKEN`, hidden input and optional at install time

If `KTDAT_TOKEN` is left empty, the app still starts, but sync returns a page-visible missing configuration error.

Remove Daed GraphQL environment prompts from docs and installer once the kt-dat sync replaces the old flow.

## Testing

Add tests for:

- CIDR extraction and `kt.txt` rendering.
- Missing GitHub config.
- GET existing file + no-op when content matches.
- GET existing file + PUT update when content differs.
- 404 GET + PUT create.
- GitHub conflict mapping.
- Server endpoint status mapping.
- Installer and README environment-variable contract.

## Non-Goals

- Do not trigger or poll `kt-dat` Actions from `kt-proxy`.
- Do not reload Daed directly from `kt-proxy`.
- Do not store GitHub tokens in the browser.
- Do not add login or user management in this change.
