# kt-proxy

`kt-proxy` is a small unauthenticated web manager for `sing-box` configuration on Ubuntu.

It reads `/etc/sing-box/config.json`, displays editable `outbounds` and `route.rules`, offers a full JSON editor, and saves only after `sing-box check` succeeds. After writing the checked config, it runs `systemctl reload sing-box`.

## Build

```bash
go build -o kt-proxy .
```

## One-Click Deploy on Ubuntu

Run this on the Ubuntu server:

```bash
curl -fsSL https://raw.githubusercontent.com/Van426326/sing-box-web/main/scripts/install.sh | sudo bash
```

The installer builds the binary from the GitHub source, installs it to `/usr/local/bin/kt-proxy`, writes `/etc/kt-proxy/kt-proxy.env`, installs `/etc/systemd/system/kt-proxy.service`, and starts the `kt-proxy` service.

During interactive install it asks for:

- `KT_PROXY_ADDR`, default `:8090`
- `SING_BOX_CONFIG_PATH`, default `/etc/sing-box/config.json`
- `KTDAT_REPO`, default `Van426326/kt-dat`
- `KTDAT_BRANCH`, default `main`
- `KTDAT_PATH`, default `kt.txt`
- `KTDAT_TOKEN`, optional at install time and hidden while typing

For non-interactive install, export the values first and preserve them through `sudo -E bash`:

```bash
export KT_PROXY_ADDR=":8090"
export SING_BOX_CONFIG_PATH="/etc/sing-box/config.json"
export KTDAT_REPO="Van426326/kt-dat"
export KTDAT_BRANCH="main"
export KTDAT_PATH="kt.txt"
export KTDAT_TOKEN="<github-token>"

curl -fsSL https://raw.githubusercontent.com/Van426326/sing-box-web/main/scripts/install.sh | sudo -E bash
```

`KTDAT_TOKEN` should be a GitHub fine-grained personal access token scoped only to `Van426326/kt-dat` with repository contents read/write permission. If `KTDAT_TOKEN` is empty, the kt-dat sync button remains available but the page will show the missing configuration error returned by the API.

Common service commands:

```bash
sudo systemctl status kt-proxy
sudo systemctl restart kt-proxy
sudo journalctl -u kt-proxy -f
```

To update, rerun the one-click install command. The script rebuilds the latest `main` branch and restarts the service.

To uninstall:

```bash
sudo systemctl disable --now kt-proxy
sudo rm -f /etc/systemd/system/kt-proxy.service /usr/local/bin/kt-proxy
sudo rm -rf /etc/kt-proxy
sudo systemctl daemon-reload
```

## Run for local development

Use the sample config as the write target so the real system config is not touched:

```bash
SING_BOX_CONFIG_PATH=./sing-box-config-example.json \
SING_BOX_EXAMPLE_PATH=./sing-box-config-example.json \
SING_BOX_BIN=/bin/true \
SYSTEMCTL_BIN=/bin/true \
KT_PROXY_ADDR=:8090 \
./kt-proxy
```

Open `http://localhost:8090`.

## Run on Ubuntu

The default runtime values are:

- `KT_PROXY_ADDR=:8090`
- `SING_BOX_CONFIG_PATH=/etc/sing-box/config.json`
- `SING_BOX_EXAMPLE_PATH=/etc/kt-proxy/sing-box-config-example.json`
- `SING_BOX_BIN=sing-box`
- `SYSTEMCTL_BIN=systemctl`

The process needs permission to:

- Read and write `/etc/sing-box/config.json`
- Create a backup next to `/etc/sing-box/config.json`
- Run `sing-box check -c <temp-file>`
- Run `systemctl reload sing-box`

## kt-dat Sync

Set these variables to enable the "同步到 kt-dat" button:

- `KTDAT_REPO`, default `Van426326/kt-dat`
- `KTDAT_BRANCH`, default `main`
- `KTDAT_PATH`, default `kt.txt`
- `KTDAT_TOKEN`

The sync reads saved sing-box `route.rules[*].ip_cidr`, writes one CIDR per line to `kt.txt`, and commits it through the GitHub Contents API. The `kt-dat` repository can then build and publish the dat file through its own Actions workflow.

## Example systemd unit

```ini
[Unit]
Description=kt-proxy sing-box web manager
After=network.target

[Service]
Type=simple
EnvironmentFile=/etc/kt-proxy/kt-proxy.env
ExecStart=/usr/local/bin/kt-proxy
Restart=on-failure
RestartSec=3

[Install]
WantedBy=multi-user.target
```

## Security

The first version has no login, no TLS, and no CSRF protection. Run it only in a trusted network, or bind `KT_PROXY_ADDR` to a private address and put your own reverse proxy or access control in front of it.
