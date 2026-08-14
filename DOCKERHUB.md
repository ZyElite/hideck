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

使用域名自动申请 HTTPS 证书，默认让 HTTPS 和 WebRTC 共用 `443` 端口：

```bash
curl -fsSL https://raw.githubusercontent.com/yibaiba/hideck/main/deploy.sh | \
  HIDECK_DOMAIN=hideck.example.com \
  HIDECK_DIR=/opt/hideck sh
```

无法开放标准端口时，使用自定义端口和 Cloudflare DNS-01：

```bash
curl -fsSL https://raw.githubusercontent.com/yibaiba/hideck/main/deploy.sh | \
  HIDECK_DOMAIN=hideck.example.com HIDECK_HTTPS_PORT=8443 \
  HIDECK_DNS_PROVIDER=cloudflare CLOUDFLARE_API_TOKEN=your_token \
  HIDECK_DIR=/opt/hideck sh
```

部署完成后通过 `https://hideck.example.com:8443` 访问。

脚本会下载 Compose、Caddy 和配置模板，创建持久化目录并拉取 `latest`；不会覆盖已有的部署文件和 `config/config.yaml`。启用 Caddy 时会生成权限为 `0600` 的 `caddy.env` 与 `hideck-caddy.env`，后续直接运行同一目录的 `deploy.sh` 仍会更新 Caddy。公网需要同时放行 `HIDECK_HTTPS_PORT` 的 TCP 和 UDP；普通证书验证还需要 `80/TCP` 或 `443/TCP`，DNS-01 不需要标准端口。

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
  webrtc_udp_address: ":7580"
  # NAT 后可复用 HTTPS 域名；同时将公网 7580/UDP 映射到本机同端口。
  webrtc_public_host: "hideck.example.com"

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

使用仓库提供的 Caddy Compose 文件时：

```dotenv
HIDECK_DOMAIN=hideck.example.com
HIDECK_HTTPS_PORT=443
```

将以上内容保存为同目录的 `caddy.env`，并创建 `hideck-caddy.env`：

```dotenv
PROXY_SERVER_WEBRTC_PUBLIC_HOST=hideck.example.com
PROXY_SERVER_WEBRTC_UDP_ADDRESS=:443
```

然后运行：

```bash
docker compose -f docker-compose.yml -f docker-compose.caddy.yml up -d
```

Web 入口为 `https://hideck.example.com`。Caddy 处理页面、API 和 WebRTC 信令；同端口 UDP 的 WebRTC 音频直接进入 HiDeck。

DNS-01 使用预构建的 `yibaiba/hideck-caddy-dns:2.11.4`，同时支持 `cloudflare`、`alidns`、`tencentcloud` 和 `route53`。分别使用 `CLOUDFLARE_API_TOKEN`，阿里云 AccessKey，腾讯云 SecretId/SecretKey，或 AWS 标准凭证环境变量。

完整的 Nginx、Caddy、Lucky IPv4/IPv6 与 WebRTC 端口配置见 [HTTPS 与 WebRTC 部署指南](docs/https-webrtc.md)。

维护该镜像时，进入仓库的 `caddy-dns-image/` 目录执行：

```bash
CADDY_VERSION=2.11.4 docker compose build --push
```

默认账号：`admin` / `admin`

首次登录后请立即修改密码。

## 维护者发布

本机使用独立的 `docker-compose.build.yml` 调用 Buildx，一次生成 amd64、arm64 和多架构 manifest。服务器部署仍只使用 `docker-compose.yml`，不会在服务器编译源码。

```bash
export HIDECK_VERSION=2.0.1
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
