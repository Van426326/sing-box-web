# kt-proxy

`kt-proxy` is a small unauthenticated web manager for `sing-box` configuration on Ubuntu.

It reads `/etc/sing-box/config.json`, displays editable `outbounds` and `route.rules`, offers a full JSON editor, and saves only after `sing-box check` succeeds. After writing the checked config, it runs `systemctl reload sing-box`.

## Build

```bash
go build -o kt-proxy .
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
- `SING_BOX_EXAMPLE_PATH=sing-box-config-example.json`
- `SING_BOX_BIN=sing-box`
- `SYSTEMCTL_BIN=systemctl`

The process needs permission to:

- Read and write `/etc/sing-box/config.json`
- Create a backup next to `/etc/sing-box/config.json`
- Run `sing-box check -c <temp-file>`
- Run `systemctl reload sing-box`

## Daed Sync

Set these variables to enable the "同步到 Daed" button:

- `DAED_GRAPHQL_URL`
- `DAED_AUTHORIZATION`

The sync reads sing-box `route.rules[*].ip_cidr` and updates the selected Daed routing block marked by `# 家 & kt` and `dip(...) -> singbox`.

## Example systemd unit

```ini
[Unit]
Description=kt-proxy sing-box web manager
After=network.target

[Service]
Type=simple
ExecStart=/usr/local/bin/kt-proxy
Environment=KT_PROXY_ADDR=:8090
Restart=on-failure

[Install]
WantedBy=multi-user.target
```

## Security

The first version has no login, no TLS, and no CSRF protection. Run it only in a trusted environment.
