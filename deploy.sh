#!/bin/sh

set -eu

SOURCE_BASE_URL="https://raw.githubusercontent.com/yibaiba/hideck/main"
MIN_PORT=1
MAX_PORT=65535

resolve_project_dir() {
  if [ -n "${HIDECK_DIR:-}" ]; then
    printf '%s\n' "$HIDECK_DIR"
    return
  fi

  script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
  if [ -f "$script_dir/docker-compose.yml" ] && [ -f "$script_dir/config/config.example.yaml" ]; then
    printf '%s\n' "$script_dir"
    return
  fi

  printf '%s/hideck\n' "$PWD"
}

PROJECT_DIR=$(resolve_project_dir)
mkdir -p "$PROJECT_DIR"
PROJECT_DIR=$(CDPATH= cd -- "$PROJECT_DIR" && pwd)
CONFIG_DIR="$PROJECT_DIR/config"
CONFIG_FILE="$CONFIG_DIR/config.yaml"
CONFIG_EXAMPLE="$CONFIG_DIR/config.example.yaml"
COMPOSE_FILE="$PROJECT_DIR/docker-compose.yml"
CADDY_COMPOSE_FILE="$PROJECT_DIR/docker-compose.caddy.yml"
CADDY_DNS_COMPOSE_FILE="$PROJECT_DIR/docker-compose.caddy-dns.yml"
CADDY_FILE="$PROJECT_DIR/Caddyfile"
CADDY_DNS_FILE="$PROJECT_DIR/Caddyfile.dns"
CADDY_DNS_DIR="$PROJECT_DIR/caddy-dns"
CADDY_ENV_FILE="$PROJECT_DIR/caddy.env"
HIDECK_CADDY_ENV_FILE="$PROJECT_DIR/hideck-caddy.env"

initialize_directories() {
  mkdir -p "$CONFIG_DIR" "$CADDY_DNS_DIR" "$PROJECT_DIR/data" "$PROJECT_DIR/logs"
}

download_if_missing() {
  target_file=$1
  source_url=$2
  file_mode=$3

  if [ -f "$target_file" ]; then
    printf '保留现有文件：%s\n' "$target_file"
    return
  fi

  command -v curl >/dev/null 2>&1 || {
    printf '缺少 curl，无法下载：%s\n' "$source_url" >&2
    exit 1
  }

  temporary_file=$(mktemp "${target_file}.tmp.XXXXXX")
  if ! curl -fsSL "$source_url" -o "$temporary_file"; then
    rm -f "$temporary_file"
    printf '下载失败：%s\n' "$source_url" >&2
    exit 1
  fi
  chmod "$file_mode" "$temporary_file"
  mv "$temporary_file" "$target_file"
  printf '已下载：%s\n' "$target_file"
}

initialize_deployment_files() {
  download_if_missing "$COMPOSE_FILE" "$SOURCE_BASE_URL/docker-compose.yml" 644
  download_if_missing "$CONFIG_EXAMPLE" "$SOURCE_BASE_URL/config/config.example.yaml" 644
  if ! caddy_requested; then
    return
  fi

  download_if_missing "$CADDY_COMPOSE_FILE" "$SOURCE_BASE_URL/docker-compose.caddy.yml" 644
  download_if_missing "$CADDY_FILE" "$SOURCE_BASE_URL/Caddyfile" 644
  if ! dns_challenge_requested; then
    return
  fi

  download_if_missing "$CADDY_DNS_COMPOSE_FILE" "$SOURCE_BASE_URL/docker-compose.caddy-dns.yml" 644
  download_if_missing "$CADDY_DNS_FILE" "$SOURCE_BASE_URL/Caddyfile.dns" 644
  download_if_missing "$CADDY_DNS_DIR/cloudflare.caddy" "$SOURCE_BASE_URL/caddy-dns/cloudflare.caddy" 644
  download_if_missing "$CADDY_DNS_DIR/alidns.caddy" "$SOURCE_BASE_URL/caddy-dns/alidns.caddy" 644
  download_if_missing "$CADDY_DNS_DIR/tencentcloud.caddy" "$SOURCE_BASE_URL/caddy-dns/tencentcloud.caddy" 644
  download_if_missing "$CADDY_DNS_DIR/route53.caddy" "$SOURCE_BASE_URL/caddy-dns/route53.caddy" 644
}

caddy_requested() {
  [ -n "${HIDECK_DOMAIN:-}" ] || [ -f "$CADDY_ENV_FILE" ]
}

dns_challenge_requested() {
  [ -n "${HIDECK_DNS_PROVIDER:-}" ] || dns_challenge_enabled
}

validate_single_line() {
  value_name=$1
  value=$2
  line_feed='
'
  carriage_return=$(printf '\r')
  case "$value" in
    *"$line_feed"*|*"$carriage_return"*)
      printf '%s 不能包含换行符。\n' "$value_name" >&2
      exit 1
      ;;
  esac
}

require_credential() {
  credential_name=$1
  credential_value=$2
  if [ -z "$credential_value" ]; then
    printf 'DNS 服务商缺少凭证：%s\n' "$credential_name" >&2
    exit 1
  fi
  validate_single_line "$credential_name" "$credential_value"
}

validate_dns_credentials() {
  provider=$1
  case "$provider" in
    "") ;;
    cloudflare)
      require_credential CLOUDFLARE_API_TOKEN "${CLOUDFLARE_API_TOKEN:-}"
      ;;
    alidns)
      require_credential ALIYUN_ACCESS_KEY_ID "${ALIYUN_ACCESS_KEY_ID:-}"
      require_credential ALIYUN_ACCESS_KEY_SECRET "${ALIYUN_ACCESS_KEY_SECRET:-}"
      ;;
    tencentcloud)
      require_credential TENCENTCLOUD_SECRET_ID "${TENCENTCLOUD_SECRET_ID:-}"
      require_credential TENCENTCLOUD_SECRET_KEY "${TENCENTCLOUD_SECRET_KEY:-}"
      ;;
    route53)
      if [ -n "${AWS_ACCESS_KEY_ID:-}" ] || [ -n "${AWS_SECRET_ACCESS_KEY:-}" ]; then
        require_credential AWS_ACCESS_KEY_ID "${AWS_ACCESS_KEY_ID:-}"
        require_credential AWS_SECRET_ACCESS_KEY "${AWS_SECRET_ACCESS_KEY:-}"
      fi
      validate_single_line AWS_SESSION_TOKEN "${AWS_SESSION_TOKEN:-}"
      validate_single_line AWS_REGION "${AWS_REGION:-}"
      ;;
    *)
      printf '不支持的 HIDECK_DNS_PROVIDER：%s\n' "$provider" >&2
      exit 1
      ;;
  esac
}

append_dns_credentials() {
  provider=$1
  target_file=$2
  case "$provider" in
    cloudflare)
      printf 'CLOUDFLARE_API_TOKEN=%s\n' "$CLOUDFLARE_API_TOKEN" >>"$target_file"
      ;;
    alidns)
      printf 'ALIYUN_ACCESS_KEY_ID=%s\n' "$ALIYUN_ACCESS_KEY_ID" >>"$target_file"
      printf 'ALIYUN_ACCESS_KEY_SECRET=%s\n' "$ALIYUN_ACCESS_KEY_SECRET" >>"$target_file"
      ;;
    tencentcloud)
      printf 'TENCENTCLOUD_SECRET_ID=%s\n' "$TENCENTCLOUD_SECRET_ID" >>"$target_file"
      printf 'TENCENTCLOUD_SECRET_KEY=%s\n' "$TENCENTCLOUD_SECRET_KEY" >>"$target_file"
      ;;
    route53)
      [ -z "${AWS_ACCESS_KEY_ID:-}" ] || printf 'AWS_ACCESS_KEY_ID=%s\n' "$AWS_ACCESS_KEY_ID" >>"$target_file"
      [ -z "${AWS_SECRET_ACCESS_KEY:-}" ] || printf 'AWS_SECRET_ACCESS_KEY=%s\n' "$AWS_SECRET_ACCESS_KEY" >>"$target_file"
      [ -z "${AWS_SESSION_TOKEN:-}" ] || printf 'AWS_SESSION_TOKEN=%s\n' "$AWS_SESSION_TOKEN" >>"$target_file"
      [ -z "${AWS_REGION:-}" ] || printf 'AWS_REGION=%s\n' "$AWS_REGION" >>"$target_file"
      ;;
  esac
}

initialize_caddy_environment() {
  domain=${HIDECK_DOMAIN:-}
  if [ -z "$domain" ]; then
    if [ -n "${HIDECK_DNS_PROVIDER:-}" ] || [ -n "${HIDECK_HTTPS_PORT:-}" ]; then
      printf 'HIDECK_DNS_PROVIDER/HIDECK_HTTPS_PORT 需要同时设置 HIDECK_DOMAIN。\n' >&2
      exit 1
    fi
    return
  fi
  case "$domain" in
    *[!A-Za-z0-9.-]*)
      printf 'HIDECK_DOMAIN 不是有效的公网域名：%s\n' "$domain" >&2
      exit 1
      ;;
  esac

  https_port=${HIDECK_HTTPS_PORT:-443}
  case "$https_port" in
    *[!0-9]*|"")
      printf 'HIDECK_HTTPS_PORT 不是有效端口：%s\n' "$https_port" >&2
      exit 1
      ;;
  esac
  if [ "$https_port" -lt "$MIN_PORT" ] || [ "$https_port" -gt "$MAX_PORT" ]; then
    printf 'HIDECK_HTTPS_PORT 超出范围：%s\n' "$https_port" >&2
    exit 1
  fi

  dns_provider=${HIDECK_DNS_PROVIDER:-}
  validate_dns_credentials "$dns_provider"

  temporary_file=$(mktemp "${CADDY_ENV_FILE}.tmp.XXXXXX")
  {
    printf 'HIDECK_DOMAIN=%s\n' "$domain"
    printf 'HIDECK_HTTPS_PORT=%s\n' "$https_port"
    [ -z "$dns_provider" ] || printf 'HIDECK_DNS_PROVIDER=%s\n' "$dns_provider"
  } >"$temporary_file"
  append_dns_credentials "$dns_provider" "$temporary_file"
  chmod 600 "$temporary_file"
  mv "$temporary_file" "$CADDY_ENV_FILE"

  temporary_file=$(mktemp "${HIDECK_CADDY_ENV_FILE}.tmp.XXXXXX")
  {
    printf 'PROXY_SERVER_WEBRTC_PUBLIC_HOST=%s\n' "$domain"
    printf 'PROXY_SERVER_WEBRTC_UDP_ADDRESS=:%s\n' "$https_port"
  } >"$temporary_file"
  chmod 600 "$temporary_file"
  mv "$temporary_file" "$HIDECK_CADDY_ENV_FILE"
  printf '已配置 Caddy：域名 %s，HTTPS/WebRTC 端口 %s\n' "$domain" "$https_port"
}

initialize_config() {
  if [ -f "$CONFIG_FILE" ]; then
    printf '保留现有配置：%s\n' "$CONFIG_FILE"
    return
  fi

  if [ ! -f "$CONFIG_EXAMPLE" ]; then
    printf '缺少配置模板：%s\n' "$CONFIG_EXAMPLE" >&2
    exit 1
  fi

  temporary_file=$(mktemp "$CONFIG_DIR/config.yaml.tmp.XXXXXX")
  cp "$CONFIG_EXAMPLE" "$temporary_file"
  chmod 600 "$temporary_file"
  mv "$temporary_file" "$CONFIG_FILE"
  printf '已创建配置：%s\n' "$CONFIG_FILE"
}

dns_challenge_enabled() {
  [ -f "$CADDY_ENV_FILE" ] &&
    awk -F= '$1 == "HIDECK_DNS_PROVIDER" && length($2) > 0 { found = 1 } END { exit !found }' "$CADDY_ENV_FILE"
}

run_compose() {
  if dns_challenge_enabled; then
    docker compose -f "$COMPOSE_FILE" -f "$CADDY_COMPOSE_FILE" -f "$CADDY_DNS_COMPOSE_FILE" \
      --project-directory "$PROJECT_DIR" "$@"
    return
  fi
  if [ -f "$CADDY_ENV_FILE" ]; then
    docker compose -f "$COMPOSE_FILE" -f "$CADDY_COMPOSE_FILE" \
      --project-directory "$PROJECT_DIR" "$@"
    return
  fi
  docker compose -f "$COMPOSE_FILE" --project-directory "$PROJECT_DIR" "$@"
}

deploy() {
  command -v docker >/dev/null 2>&1 || {
    printf '缺少 Docker，无法部署 HiDeck。\n' >&2
    exit 1
  }
  docker compose version >/dev/null
  run_compose pull hideck
  if [ -f "$CADDY_ENV_FILE" ]; then
    run_compose pull caddy
  fi
  run_compose up -d --remove-orphans
  run_compose ps
}

initialize_directories
initialize_deployment_files
initialize_caddy_environment
initialize_config
deploy
