#!/bin/sh

set -eu

PROJECT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
CONFIG_DIR="$PROJECT_DIR/config"
CONFIG_FILE="$CONFIG_DIR/config.yaml"
CONFIG_EXAMPLE="$CONFIG_DIR/config.example.yaml"

initialize_directories() {
  mkdir -p "$CONFIG_DIR" "$PROJECT_DIR/data" "$PROJECT_DIR/logs"
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

  cp "$CONFIG_EXAMPLE" "$CONFIG_FILE"
  chmod 600 "$CONFIG_FILE"
  printf '已创建配置：%s\n' "$CONFIG_FILE"
}

run_compose() {
  docker compose -f "$PROJECT_DIR/docker-compose.yml" --project-directory "$PROJECT_DIR" "$@"
}

deploy() {
  run_compose pull
  run_compose up -d --remove-orphans
  run_compose ps
}

initialize_directories
initialize_config
deploy
