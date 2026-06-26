#!/usr/bin/env bash
set -euo pipefail

REPO="${KT_PROXY_REPO:-Van426326/sing-box-web}"
BRANCH="${KT_PROXY_INSTALL_BRANCH:-main}"
BIN_PATH="/usr/local/bin/kt-proxy"
CONFIG_DIR="/etc/kt-proxy"
ENV_FILE="/etc/kt-proxy/kt-proxy.env"
SERVICE_FILE="/etc/systemd/system/kt-proxy.service"
EXAMPLE_FILE="/etc/kt-proxy/sing-box-config-example.json"
WORK_DIR="$(mktemp -d)"

cleanup() {
  rm -rf "$WORK_DIR"
}
trap cleanup EXIT

log() {
  printf '\n==> %s\n' "$1"
}

fail() {
  printf '错误: %s\n' "$1" >&2
  exit 1
}

require_root() {
  if [ "$(id -u)" -ne 0 ]; then
    fail "请使用 root 权限运行，例如: curl -fsSL https://raw.githubusercontent.com/${REPO}/${BRANCH}/scripts/install.sh | sudo bash"
  fi
}

require_ubuntu() {
  if [ ! -r /etc/os-release ]; then
    fail "无法读取 /etc/os-release，仅支持 Ubuntu"
  fi

  ID=""
  # shellcheck disable=SC1091
  . /etc/os-release
  if [ "$ID" != "ubuntu" ]; then
    fail "当前系统 ID=${ID:-unknown}，此安装脚本仅支持 Ubuntu"
  fi
}

prompt_value() {
  local var_name="$1"
  local prompt="$2"
  local default_value="$3"
  local current_value="${!var_name:-}"
  local input=""

  if [ -n "$current_value" ]; then
    printf -v "$var_name" '%s' "$current_value"
    return
  fi

  if [ -r /dev/tty ]; then
    if [ -n "$default_value" ]; then
      printf '%s [%s]: ' "$prompt" "$default_value" > /dev/tty
    else
      printf '%s: ' "$prompt" > /dev/tty
    fi
    IFS= read -r input < /dev/tty || input=""
  fi

  if [ -z "$input" ]; then
    input="$default_value"
  fi
  printf -v "$var_name" '%s' "$input"
}

load_existing_env() {
  if [ ! -r "$ENV_FILE" ]; then
    return
  fi

  local key value
  while IFS='=' read -r key value; do
    case "$key" in
      KT_PROXY_ADDR|SING_BOX_CONFIG_PATH|KTDAT_REPO|KTDAT_BRANCH|KTDAT_PATH|KTDAT_TOKEN)
        value="${value#\'}"
        value="${value%\'}"
        printf -v "CURRENT_$key" '%s' "$value"
        ;;
    esac
  done < "$ENV_FILE"
}

prompt_secret() {
  local var_name="$1"
  local prompt="$2"
  local default_value="${3:-}"
  local current_value="${!var_name:-}"
  local input=""

  if [ -n "$current_value" ]; then
    printf -v "$var_name" '%s' "$current_value"
    return
  fi

  if [ -r /dev/tty ]; then
    printf '%s: ' "$prompt" > /dev/tty
    IFS= read -r -s input < /dev/tty || input=""
    printf '\n' > /dev/tty
  fi

  if [ -z "$input" ]; then
    input="$default_value"
  fi
  printf -v "$var_name" '%s' "$input"
}

quote_env() {
  printf "'%s'" "$(printf '%s' "$1" | sed "s/'/'\\\\''/g")"
}

install_dependencies() {
  log "安装构建依赖"
  apt-get update

  local packages=(ca-certificates curl tar)
  if ! command -v go >/dev/null 2>&1; then
    packages+=(golang-go)
  fi

  DEBIAN_FRONTEND=noninteractive apt-get install -y "${packages[@]}"
}

download_and_build() {
  log "下载源码并构建 kt-proxy"
  local archive="$WORK_DIR/src.tar.gz"
  curl -fsSL "https://github.com/${REPO}/archive/refs/heads/${BRANCH}.tar.gz" -o "$archive"
  tar -xzf "$archive" -C "$WORK_DIR"

  local source_dir="$WORK_DIR/$(printf '%s' "$REPO" | awk -F/ '{print $2}')-${BRANCH}"
  if [ ! -d "$source_dir" ]; then
    source_dir="$(find "$WORK_DIR" -mindepth 1 -maxdepth 1 -type d | head -n 1)"
  fi
  if [ ! -d "$source_dir" ]; then
    fail "源码解压失败"
  fi

  (cd "$source_dir" && go build -o "$WORK_DIR/kt-proxy" .)
  install -m 0755 "$WORK_DIR/kt-proxy" "$BIN_PATH"
  install -m 0644 "$source_dir/sing-box-config-example.json" "$EXAMPLE_FILE"
}

write_env_file() {
  log "写入环境变量配置"
  install -d -m 0755 "$CONFIG_DIR"
  load_existing_env

  prompt_value KT_PROXY_ADDR "kt-proxy 监听地址" "${CURRENT_KT_PROXY_ADDR:-:8090}"
  prompt_value SING_BOX_CONFIG_PATH "sing-box 配置文件路径" "${CURRENT_SING_BOX_CONFIG_PATH:-/etc/sing-box/config.json}"
  prompt_value KTDAT_REPO "kt-dat GitHub 仓库" "${CURRENT_KTDAT_REPO:-Van426326/kt-dat}"
  prompt_value KTDAT_BRANCH "kt-dat 分支" "${CURRENT_KTDAT_BRANCH:-main}"
  prompt_value KTDAT_PATH "kt-dat CIDR 文件路径" "${CURRENT_KTDAT_PATH:-kt.txt}"
  prompt_secret KTDAT_TOKEN "GitHub Token（可留空，输入不会显示；直接回车保留旧值）" "${CURRENT_KTDAT_TOKEN:-}"

  umask 077
  {
    printf 'KT_PROXY_ADDR=%s\n' "$(quote_env "$KT_PROXY_ADDR")"
    printf 'SING_BOX_CONFIG_PATH=%s\n' "$(quote_env "$SING_BOX_CONFIG_PATH")"
    printf 'SING_BOX_EXAMPLE_PATH=%s\n' "$(quote_env "$EXAMPLE_FILE")"
    printf 'SING_BOX_BIN=%s\n' "$(quote_env "${SING_BOX_BIN:-sing-box}")"
    printf 'SYSTEMCTL_BIN=%s\n' "$(quote_env "${SYSTEMCTL_BIN:-systemctl}")"
    printf 'KTDAT_REPO=%s\n' "$(quote_env "$KTDAT_REPO")"
    printf 'KTDAT_BRANCH=%s\n' "$(quote_env "$KTDAT_BRANCH")"
    printf 'KTDAT_PATH=%s\n' "$(quote_env "$KTDAT_PATH")"
    printf 'KTDAT_TOKEN=%s\n' "$(quote_env "$KTDAT_TOKEN")"
  } > "$ENV_FILE"
  chmod 0600 "$ENV_FILE"
}

write_service_file() {
  log "安装 systemd 服务"
  cat > "$SERVICE_FILE" <<EOF
[Unit]
Description=kt-proxy sing-box web manager
After=network.target

[Service]
Type=simple
EnvironmentFile=$ENV_FILE
ExecStart=$BIN_PATH
Restart=on-failure
RestartSec=3

[Install]
WantedBy=multi-user.target
EOF
  chmod 0644 "$SERVICE_FILE"

  systemctl daemon-reload
  systemctl enable kt-proxy
  systemctl restart kt-proxy
}

print_summary() {
  log "安装完成"
  if [[ "$KT_PROXY_ADDR" == :* ]]; then
    printf '访问地址: http://<服务器IP>%s\n' "$KT_PROXY_ADDR"
  else
    printf '监听地址: %s\n' "$KT_PROXY_ADDR"
  fi
  printf '环境变量: %s\n' "$ENV_FILE"
  printf '服务状态: systemctl status kt-proxy\n'
  printf '服务日志: journalctl -u kt-proxy -f\n'
  systemctl --no-pager status kt-proxy || true
}

main() {
  require_root
  require_ubuntu
  install_dependencies
  write_env_file
  download_and_build
  write_service_file
  print_summary
}

main "$@"
