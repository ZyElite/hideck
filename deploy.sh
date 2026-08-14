#!/bin/sh

set -eu

SOURCE_BASE_URL="https://raw.githubusercontent.com/yibaiba/hideck/main"

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

initialize_directories() {
  mkdir -p "$CONFIG_DIR" "$PROJECT_DIR/data" "$PROJECT_DIR/logs"
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

run_compose() {
  docker compose -f "$COMPOSE_FILE" --project-directory "$PROJECT_DIR" "$@"
}

deploy() {
  command -v docker >/dev/null 2>&1 || {
    printf '缺少 Docker，无法部署 HiDeck。\n' >&2
    exit 1
  }
  docker compose version >/dev/null
  run_compose pull
  run_compose up -d --remove-orphans
  run_compose ps
}

initialize_directories
initialize_deployment_files
initialize_config
deploy
