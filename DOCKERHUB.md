# HiDeck Docker Hub 镜像

镜像地址：`yibaiba/hideck`

支持架构：

- `linux/amd64`
- `linux/arm64`

## 快速启动（推荐）

直接通过 curl 运行部署脚本，默认安装到当前目录下的 `hideck/`：

```bash
curl -fsSL https://raw.githubusercontent.com/yibaiba/hideck/main/deploy.sh | sh
```

自定义安装目录：

```bash
curl -fsSL https://raw.githubusercontent.com/yibaiba/hideck/main/deploy.sh | HIDECK_DIR=/opt/hideck sh
```

脚本会下载 `docker-compose.yml` 和配置模板，创建持久化目录并拉取 `latest`；不会覆盖已有的部署文件和 `config/config.yaml`。

## 手工部署

```bash
mkdir -p hideck/{config,data,logs}
cd hideck
```

创建 `config/config.yaml`：

```yaml
server:
  port: 7575
  debug: false
  https_enabled: false

web:
  username: admin
  password: admin

devices: []

proxy:
  instances: []

vowifi:
  enabled: false
```

创建 `docker-compose.yml`：

```yaml
services:
  hideck:
    image: yibaiba/hideck:latest
    container_name: hideck
    restart: unless-stopped
    init: true
    stop_grace_period: 30s
    network_mode: host
    privileged: true
    volumes:
      - ./config:/app/config
      - ./data:/app/data
      - ./logs:/app/logs
      - /dev:/dev
    environment:
      TZ: Asia/Shanghai
      CONFIG_PATH: /app/config/config.yaml
    logging:
      driver: json-file
      options:
        max-size: 10m
        max-file: "3"
```

启动：

```bash
docker compose up -d
```

Web 入口：`http://YOUR_IP:7575`

默认账号：`admin` / `admin`

首次登录后请立即修改密码。

## 维护者发布

本机使用独立的 `docker-compose.build.yml` 调用 Buildx，一次生成 amd64、arm64 和多架构 manifest。服务器部署仍只使用 `docker-compose.yml`，不会在服务器编译源码。

```bash
export HIDECK_VERSION=2.0.13
export HIDECK_MINOR_VERSION=2.0
export HIDECK_REVISION="$(git rev-parse HEAD)"
export HIDECK_BUILDTIME="$(date -u +'%Y-%m-%dT%H:%M:%SZ')"

docker compose -f docker-compose.build.yml build --check
docker compose -f docker-compose.build.yml build --push

docker buildx imagetools inspect "yibaiba/hideck:${HIDECK_VERSION}"
```

正式发布必须从干净、已提交的源码快照构建，确保镜像标签可以追溯到 `REVISION`。

## 更新镜像

```bash
docker compose pull
docker compose up -d
```

应用内二进制热更新在这个源码整合构建中已禁用。Docker 部署请通过拉取新镜像升级。

## 配置说明

| 路径 | 说明 |
| --- | --- |
| `/app/config` | 配置文件目录 |
| `/app/data` | SQLite 数据与运行数据 |
| `/app/logs` | 日志目录 |

容器默认时区为 `Asia/Shanghai`。Compose 文件也显式设置了同一时区，方便在不同运行方式下保持一致。

## 许可证提示

本仓库是源码整合树，不是单一 MIT 许可项目。根项目来自 PolyForm Noncommercial 1.0.0，`third_party/vowifi-go` 为 AGPL-3.0，其它第三方源码按各自许可证授权。发布公开二进制或 Docker 镜像前，请先确认组合分发的许可证义务。
