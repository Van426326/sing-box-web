# Ubuntu Install Script Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a one-command Ubuntu installer that builds `kt-proxy`, installs a systemd service, records Daed environment variables, and documents deployment.

**Architecture:** The installer is a Bash script under `scripts/install.sh` that downloads the GitHub source tarball, builds the Go binary locally, writes `/etc/kt-proxy/kt-proxy.env`, installs `/etc/systemd/system/kt-proxy.service`, and starts the service. A root-package Go test treats the shell script and README as release artifacts and checks the important deployment contract text and commands.

**Tech Stack:** Bash, systemd, apt, curl, tar, Go 1.22 project tests.

## Global Constraints

- Target operating system is Ubuntu.
- Daed configuration is provided only through `DAED_GRAPHQL_URL` and `DAED_AUTHORIZATION`.
- `DAED_AUTHORIZATION` must be prompted with hidden input.
- The deployment command must support `curl | sudo bash`.
- The service must run `/usr/local/bin/kt-proxy`.
- Do not commit built binaries or secrets.

---

### Task 1: Installer Contract Test

**Files:**
- Create: `install_script_test.go`

**Interfaces:**
- Consumes: `scripts/install.sh` and `README.md` as text files.
- Produces: `TestInstallScriptDocumentsAndImplementsUbuntuDeployment`.

- [ ] **Step 1: Write the failing test**

Create `install_script_test.go` with a test that reads `scripts/install.sh` and `README.md`, then checks for the Ubuntu guard, GitHub download URL, hidden Daed authorization prompt, env file path, systemd service path, daemon reload, `enable --now`, and README curl command.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./...`
Expected: FAIL because `scripts/install.sh` is missing and README has no one-click deploy command.

- [ ] **Step 3: Proceed to Task 2 after observing the failure**

No implementation belongs in this task.

### Task 2: Ubuntu Installer Script

**Files:**
- Create: `scripts/install.sh`

**Interfaces:**
- Consumes: GitHub repo `Van426326/sing-box-web`, branch `${KT_PROXY_INSTALL_BRANCH:-main}`, source file `sing-box-config-example.json`.
- Produces: `/usr/local/bin/kt-proxy`, `/etc/kt-proxy/kt-proxy.env`, `/etc/kt-proxy/sing-box-config-example.json`, `/etc/systemd/system/kt-proxy.service`.

- [ ] **Step 1: Implement root and Ubuntu checks**

Use `set -euo pipefail`, require `id -u` equals `0`, source `/etc/os-release`, and exit unless `ID=ubuntu`.

- [ ] **Step 2: Implement prompt and env-file helpers**

Add helpers to prompt defaults for `KT_PROXY_ADDR` and `SING_BOX_CONFIG_PATH`, normal prompt for `DAED_GRAPHQL_URL`, hidden prompt for `DAED_AUTHORIZATION`, and shell-safe single-quote output for environment values.

- [ ] **Step 3: Install dependencies and build**

Run `apt-get update`, install `ca-certificates curl tar golang-go`, download `https://github.com/Van426326/sing-box-web/archive/refs/heads/${BRANCH}.tar.gz`, extract, run `go build -o "$WORK_DIR/kt-proxy" .`, and install the binary to `/usr/local/bin/kt-proxy`.

- [ ] **Step 4: Write service files and start systemd service**

Create `/etc/kt-proxy`, write env file mode `0600`, copy the sample config mode `0644`, write the systemd unit, run `systemctl daemon-reload`, then `systemctl enable --now kt-proxy`.

- [ ] **Step 5: Run script syntax verification**

Run: `bash -n scripts/install.sh`
Expected: PASS with exit code 0.

### Task 3: Deployment Documentation

**Files:**
- Modify: `README.md`

**Interfaces:**
- Consumes: installer behavior from Task 2.
- Produces: deployment instructions for interactive install, non-interactive install, paths, service commands, update, uninstall, and security notes.

- [ ] **Step 1: Document one-click install**

Add `curl -fsSL https://raw.githubusercontent.com/Van426326/sing-box-web/main/scripts/install.sh | sudo bash`.

- [ ] **Step 2: Document non-interactive install**

Show exporting `KT_PROXY_ADDR`, `SING_BOX_CONFIG_PATH`, `DAED_GRAPHQL_URL`, and `DAED_AUTHORIZATION`, then piping the installer into `sudo -E bash`.

- [ ] **Step 3: Document operational commands**

Include `systemctl status/restart`, `journalctl`, env file path, update by rerunning install, and uninstall commands.

- [ ] **Step 4: Run full verification**

Run: `go test ./...`
Expected: PASS.
