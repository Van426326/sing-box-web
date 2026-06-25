# sing-box Web Manager Design

Date: 2026-06-25

## Goal

Build a lightweight web manager that runs on Ubuntu and lets users view, add, edit, and save `/etc/sing-box/config.json`.

The first version is intentionally unauthenticated. It is expected to be run in a trusted environment. Authentication, role control, and remote hardening are out of scope for this version.

## Technology Choice

Use a Go single binary with embedded static HTML, CSS, and JavaScript.

Reasons:

- Simple Ubuntu deployment: copy one executable and run it as a systemd service.
- Go can safely coordinate file reads, atomic writes, backups, and system commands.
- No Node.js or Python runtime is required on the target host.
- Static frontend files can be embedded with `embed.FS`.

## Runtime Configuration

Defaults:

- Listen address: `:8090`
- Config path: `/etc/sing-box/config.json`
- Example fallback path: `sing-box-config-example.json`

Environment overrides:

- `KT_PROXY_ADDR`
- `SING_BOX_CONFIG_PATH`
- `SING_BOX_EXAMPLE_PATH`
- `SING_BOX_BIN`
- `SYSTEMCTL_BIN`

The example fallback is for development and recovery display only. Saving always targets the configured config path.

## User Interface

The first screen is the working application, not a landing page.

Views:

- Overview: config path, load status, last load time, outbound count, route rule count, and `route.final`.
- Outbounds: searchable table for `outbounds`, with add, edit, delete, and duplicate actions.
- Route Rules: ordered table for `route.rules`, with add, edit, delete, duplicate, and move up/down actions.
- Full JSON: formatted JSON editor for the entire config, used as an advanced fallback.

Friendly forms focus on common fields from the sample config:

- Outbounds: `type`, `tag`, `server`, `server_port`, `version`, `network`, `bind_interface`.
- Route rules: `action`, `outbound`, `protocol`, `ip_cidr`, `domain`, `domain_suffix`, `domain_keyword`, `rule_set`, `strategy`.

Unknown or complex objects remain editable through an object-level JSON text area so the UI does not discard fields it does not understand.

## Data Flow

Load:

1. Browser requests `GET /api/config`.
2. Backend reads the configured sing-box config path.
3. If the configured path cannot be read, backend tries the example config path and marks the response as fallback data.
4. Backend returns parsed JSON plus metadata.

Edit:

1. Browser keeps one canonical in-memory config object.
2. Form edits mutate the canonical object.
3. The Full JSON view can replace the canonical object after JSON parsing succeeds.

Save:

1. Browser sends the full config object to `POST /api/config/save`.
2. Backend parses and pretty-prints the JSON.
3. Backend writes a temporary config file.
4. Backend runs `sing-box check -c <temporary-file>`.
5. If check fails, the real config file is not modified.
6. If check passes, backend creates a timestamped backup next to the real config file.
7. Backend writes the new config to the real config path.
8. Backend runs `systemctl reload sing-box`.
9. Backend returns check output, reload output, and backup path.

## Error Handling

- Invalid JSON from the browser returns HTTP 400 and does not run `sing-box check`.
- `sing-box check` failure returns HTTP 422 and does not overwrite the real config file.
- Backup or write failure returns HTTP 500 and includes the system error.
- Reload failure returns HTTP 500 after writing the checked config. The response includes the backup path so the user can manually restore.
- Load fallback responses clearly say that the displayed data came from the example file.

## Security Notes

The first version has no login and no CSRF protection. It should run only in a trusted environment.

Because it writes `/etc/sing-box/config.json` and calls `systemctl reload sing-box`, the recommended deployment is a systemd service with the privileges required to perform those actions.

## Testing

Backend tests:

- Loading from primary path.
- Loading from example fallback.
- Save rejects invalid JSON.
- Save does not overwrite the real config when `sing-box check` fails.
- Save creates a backup and writes the new config when check succeeds.
- Reload failure reports the backup path.

Manual UI verification:

- Open the app.
- Load the sample config.
- Add and edit a socks outbound.
- Add, edit, delete, and reorder route rules.
- Edit the full JSON and confirm the structured views update.
- Save with mocked or local command paths during development.

## Out of Scope

- Login, TLS, users, and permissions.
- Subscription management.
- Automatic rollback after reload failure.
- Support for every sing-box schema field as a specialized form.
- Browser test automation.
