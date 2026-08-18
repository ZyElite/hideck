#!/bin/sh

set -eu

REPO="yibaiba/hideck"
SOURCE_BASE_URL="https://raw.githubusercontent.com/yibaiba/hideck/main"
RELEASES_API_URL="https://api.github.com/repos/${REPO}/releases/latest"
RELEASES_LATEST_URL="https://github.com/${REPO}/releases/latest"
RELEASES_DOWNLOAD_URL="https://github.com/${REPO}/releases/download"
USER_AGENT="hideck-deploy-binary/1.0 (+https://github.com/${REPO})"

require_command() {
  command -v "$1" >/dev/null 2>&1 || {
    printf '缺少 %s，无法部署 HiDeck。\n' "$1" >&2
    exit 1
  }
}

resolve_project_dir() {
  if [ -n "${HIDECK_DIR:-}" ]; then
    printf '%s\n' "$HIDECK_DIR"
    return
  fi

  script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
  if [ -f "$script_dir/config/config.example.yaml" ] && [ -f "$script_dir/deploy-binary.sh" ]; then
    printf '%s\n' "$script_dir"
    return
  fi

  printf '%s/hideck\n' "$PWD"
}

detect_os() {
  os=$(uname -s)
  case "$os" in
    Linux) ;;
    *)
      printf '二进制部署脚本只支持 Linux，当前系统：%s\n' "$os" >&2
      exit 1
      ;;
  esac
}

detect_arch() {
  if [ -n "${HIDECK_ARCH:-}" ]; then
    case "$HIDECK_ARCH" in
      linux_amd64|amd64|x86_64) printf 'linux_amd64\n' ;;
      linux_arm64|arm64|aarch64) printf 'linux_arm64\n' ;;
      linux_armv7|armv7|armv7l|armhf) printf 'linux_armv7\n' ;;
      *)
        printf '不支持的 HIDECK_ARCH：%s\n' "$HIDECK_ARCH" >&2
        exit 1
        ;;
    esac
    return
  fi

  machine=$(uname -m)
  case "$machine" in
    x86_64|amd64) printf 'linux_amd64\n' ;;
    aarch64|arm64) printf 'linux_arm64\n' ;;
    armv7l|armv7|armhf) printf 'linux_armv7\n' ;;
    *)
      printf '无法识别的 CPU 架构：%s\n请设置 HIDECK_ARCH=linux_amd64|linux_arm64|linux_armv7\n' "$machine" >&2
      exit 1
      ;;
  esac
}

curl_github() {
  curl -fL --retry 3 --retry-delay 1 -A "$USER_AGENT" "$@"
}

normalize_version() {
  version=$1
  case "$version" in
    ""|latest) printf 'latest\n' ;;
    v*) printf '%s\n' "$version" ;;
    *) printf 'v%s\n' "$version" ;;
  esac
}

extract_tag_name() {
  printf '%s\n' "$1" | sed -n 's#.*/releases/tag/\([^/?#]*\).*#\1#p' | head -n 1
}

resolve_latest_version() {
  latest=$( { curl_github -sS "$RELEASES_API_URL" || true; } | sed -n 's/.*"tag_name":[[:space:]]*"\([^"]*\)".*/\1/p' | head -n 1)
  if [ -n "$latest" ]; then
    printf '%s\n' "$latest"
    return
  fi

  latest=$(extract_tag_name "$(curl_github -sS -o /dev/null -w '%{url_effective}' "$RELEASES_LATEST_URL" || true)")
  if [ -n "$latest" ]; then
    printf '%s\n' "$latest"
    return
  fi

  printf '无法从 GitHub Releases 读取最新版本。\n可设置 HIDECK_VERSION=v2.0.11 后重试。\n' >&2
  exit 1
}

resolve_version() {
  requested=$(normalize_version "${HIDECK_VERSION:-latest}")
  if [ "$requested" != "latest" ]; then
    printf '%s\n' "$requested"
    return
  fi
  resolve_latest_version
}

download_file() {
  source_url=$1
  target_file=$2
  file_mode=$3

  require_command curl
  temporary_file=$(mktemp "${target_file}.tmp.XXXXXX")
  if ! curl_github "$source_url" -o "$temporary_file"; then
    rm -f "$temporary_file"
    printf '下载失败：%s\n' "$source_url" >&2
    exit 1
  fi
  chmod "$file_mode" "$temporary_file"
  mv "$temporary_file" "$target_file"
}

try_download_file() {
  source_url=$1
  target_file=$2
  file_mode=$3

  temporary_file=$(mktemp "${target_file}.tmp.XXXXXX")
  if curl_github "$source_url" -o "$temporary_file"; then
    chmod "$file_mode" "$temporary_file"
    mv "$temporary_file" "$target_file"
    return 0
  fi
  rm -f "$temporary_file"
  return 1
}

download_if_missing() {
  target_file=$1
  source_url=$2
  file_mode=$3

  if [ -f "$target_file" ]; then
    printf '保留现有文件：%s\n' "$target_file"
    return
  fi
  download_file "$source_url" "$target_file" "$file_mode"
  printf '已下载：%s\n' "$target_file"
}

file_sha256() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
    return
  fi
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
    return
  fi
  printf '缺少 sha256sum 或 shasum，无法校验下载文件。\n' >&2
  exit 1
}

checksum_for_asset() {
  sums_file=$1
  asset_name=$2
  awk -v name="$asset_name" '
    NF >= 2 {
      n = $NF
      sub(/^.*\//, "", n)
      if (n == name) {
        print $1
        exit
      }
    }
  ' "$sums_file"
}

verify_checksum() {
  sums_file=$1
  sidecar_file=$2
  asset_name=$3
  binary_file=$4

  actual=$(file_sha256 "$binary_file")
  expected=""
  source_name=""

  if [ -f "$sidecar_file" ]; then
    expected=$(checksum_for_asset "$sidecar_file" "$asset_name")
    source_name="${asset_name}.sha256"
  fi
  if [ -z "$expected" ] && [ -f "$sums_file" ]; then
    expected=$(checksum_for_asset "$sums_file" "$asset_name")
    source_name="SHA256SUMS"
  fi
  if [ -z "$expected" ]; then
    printf '找不到 %s 的校验文件（SHA256SUMS 或 %s.sha256）。\n' "$asset_name" "$asset_name" >&2
    exit 1
  fi
  if [ "$expected" != "$actual" ]; then
    printf '校验失败：%s\n来源 %s\n期望 %s\n实际 %s\n文件可能还在上传，或校验清单和二进制不是同一批。请稍后重试，或设置 HIDECK_VERSION 指定版本。\n' \
      "$asset_name" "$source_name" "$expected" "$actual" >&2
    exit 1
  fi
  printf '校验通过：%s（%s）\n' "$asset_name" "$source_name"
}

write_systemd_unit() {
  unit_file=$1
  working_dir=$2
  binary_path=$3
  config_file=$4

  temporary_file=$(mktemp "${unit_file}.tmp.XXXXXX")
  cat >"$temporary_file" <<EOF
[Unit]
Description=HiDeck modem management service
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
WorkingDirectory=${working_dir}
ExecStart=${binary_path} -c ${config_file}
Restart=on-failure
RestartSec=5s
TimeoutStopSec=15s

[Install]
WantedBy=multi-user.target
EOF
  chmod 644 "$temporary_file"
  mv "$temporary_file" "$unit_file"
}

maybe_sudo() {
  if [ "$(id -u)" -eq 0 ]; then
    "$@"
    return
  fi
  if command -v sudo >/dev/null 2>&1 && sudo -n true >/dev/null 2>&1; then
    sudo "$@"
    return
  fi
  return 1
}

install_systemd_service() {
  unit_source=$1
  if ! command -v systemctl >/dev/null 2>&1; then
    printf '未检测到 systemd。可手动运行：\n  %s -c %s\n' "$BINARY_PATH" "$CONFIG_FILE"
    return
  fi

  if maybe_sudo install -m 644 "$unit_source" /etc/systemd/system/hideck.service; then
    maybe_sudo systemctl daemon-reload
    maybe_sudo systemctl enable --now hideck.service
    maybe_sudo systemctl --no-pager --full status hideck.service || true
    printf '已安装并启动 systemd 服务：hideck.service\n'
    return
  fi

  printf '没有 systemd 写入权限。单元文件已生成：%s\n可用下面命令安装：\n  sudo cp %s /etc/systemd/system/hideck.service\n  sudo systemctl daemon-reload\n  sudo systemctl enable --now hideck.service\n或前台运行：\n  %s -c %s\n' \
    "$unit_source" "$unit_source" "$BINARY_PATH" "$CONFIG_FILE"
}

install_recording_libraries() {
  printf '正在安装通话录音依赖（AMR/MP3）…\n'
  if install_recording_libraries_with_pkg; then
    printf '录音依赖已安装。\n'
    return 0
  fi
  printf '录音依赖未装全。打电话不受影响，只是不会生成 MP3/渠道语音。\n也可改用 Docker：\n  curl -fsSL https://raw.githubusercontent.com/yibaiba/hideck/main/deploy.sh | sh\n'
  return 1
}

install_packages() {
  installer=$1
  shift
  failed=0
  for package in "$@"; do
    if ! $installer "$package"; then
      printf '未安装：%s\n' "$package"
      failed=1
    fi
  done
  return "$failed"
}

install_recording_libraries_with_pkg() {
  if command -v apt-get >/dev/null 2>&1; then
    maybe_sudo apt-get update -y || return 1
    install_packages "maybe_sudo apt-get install -y" \
      libmp3lame0 libopencore-amrnb0 libopencore-amrwb0 libvo-amrwbenc0
    return $?
  fi
  if command -v dnf >/dev/null 2>&1; then
    install_packages "maybe_sudo dnf install -y" lame-libs opencore-amr vo-amrwbenc
    return $?
  fi
  if command -v yum >/dev/null 2>&1; then
    install_packages "maybe_sudo yum install -y" lame-libs opencore-amr vo-amrwbenc
    return $?
  fi
  if command -v apk >/dev/null 2>&1; then
    install_packages "maybe_sudo apk add --no-cache" lame-libs opencore-amr vo-amrwbenc
    return $?
  fi
  if command -v pacman >/dev/null 2>&1; then
    install_packages "maybe_sudo pacman -Sy --noconfirm" lame opencore-amr vo-amrwbenc
    return $?
  fi
  if command -v opkg >/dev/null 2>&1; then
    maybe_sudo opkg update || return 1
    install_packages "maybe_sudo opkg install" \
      lame-lib libopencore-amrnb libopencore-amrwb libvo-amrwbenc
    return $?
  fi
  printf '未识别的包管理器，请自行安装 libmp3lame、libopencore-amrnb、libopencore-amrwb、libvo-amrwbenc。\n'
  return 1
}

detect_os
require_command curl
require_command uname

PROJECT_DIR=$(resolve_project_dir)
mkdir -p "$PROJECT_DIR"
PROJECT_DIR=$(CDPATH= cd -- "$PROJECT_DIR" && pwd)
CONFIG_DIR="$PROJECT_DIR/config"
CONFIG_FILE="$CONFIG_DIR/config.yaml"
CONFIG_EXAMPLE="$CONFIG_DIR/config.example.yaml"
BIN_DIR="$PROJECT_DIR"
BINARY_PATH="$BIN_DIR/hideck"
UNIT_FILE="$PROJECT_DIR/hideck.service"

ARCH=$(detect_arch)
VERSION=$(resolve_version)
ASSET_NAME="hideck_${VERSION}_${ARCH}"
ASSET_URL="${RELEASES_DOWNLOAD_URL}/${VERSION}/${ASSET_NAME}"
SUMS_URL="${RELEASES_DOWNLOAD_URL}/${VERSION}/SHA256SUMS"
SIDECAR_URL="${RELEASES_DOWNLOAD_URL}/${VERSION}/${ASSET_NAME}.sha256"

printf '部署目录：%s\n版本：%s\n架构：%s\n' "$PROJECT_DIR" "$VERSION" "$ARCH"

mkdir -p "$CONFIG_DIR" "$PROJECT_DIR/data" "$PROJECT_DIR/logs"
download_if_missing "$CONFIG_EXAMPLE" "$SOURCE_BASE_URL/config/config.example.yaml" 644
if [ -f "$CONFIG_FILE" ]; then
  printf '保留现有配置：%s\n' "$CONFIG_FILE"
else
  if [ ! -f "$CONFIG_EXAMPLE" ]; then
    printf '缺少配置模板：%s\n' "$CONFIG_EXAMPLE" >&2
    exit 1
  fi
  temporary_file=$(mktemp "$CONFIG_DIR/config.yaml.tmp.XXXXXX")
  cp "$CONFIG_EXAMPLE" "$temporary_file"
  chmod 600 "$temporary_file"
  mv "$temporary_file" "$CONFIG_FILE"
  printf '已创建配置：%s\n' "$CONFIG_FILE"
fi

DOWNLOAD_DIR=$(mktemp -d "${TMPDIR:-/tmp}/hideck-binary.XXXXXX")
trap 'rm -rf "$DOWNLOAD_DIR"' EXIT
download_file "$ASSET_URL" "$DOWNLOAD_DIR/$ASSET_NAME" 755
if ! try_download_file "$SIDECAR_URL" "$DOWNLOAD_DIR/${ASSET_NAME}.sha256" 644; then
  printf '未找到 %s.sha256，改用 SHA256SUMS。\n' "$ASSET_NAME"
fi
if ! try_download_file "$SUMS_URL" "$DOWNLOAD_DIR/SHA256SUMS" 644; then
  printf '未找到 SHA256SUMS。\n'
fi
verify_checksum "$DOWNLOAD_DIR/SHA256SUMS" "$DOWNLOAD_DIR/${ASSET_NAME}.sha256" "$ASSET_NAME" "$DOWNLOAD_DIR/$ASSET_NAME"

install -m 755 "$DOWNLOAD_DIR/$ASSET_NAME" "$BINARY_PATH"
printf '已安装二进制：%s\n' "$BINARY_PATH"
install_recording_libraries || true

write_systemd_unit "$UNIT_FILE" "$PROJECT_DIR" "$BINARY_PATH" "$CONFIG_FILE"
install_systemd_service "$UNIT_FILE"

printf '\n浏览器打开：http://YOUR_IP:7575\n默认账号：admin / admin，首次登录后请立即改密。\n'
