# WOTM

轻量的 Web SSH 工作台，支持 Linux 主机管理、SSH 密钥管理、多标签终端和文件上传。所有数据保存在一个 YAML 配置文件中，无需数据库。

项目地址：[vhvy/wotm](https://github.com/vhvy/wotm)。

## 部署

安装 Docker 和 Docker Compose，将 [docker-compose.prod.yaml](docker-compose.prod.yaml) 保存到部署目录。镜像发布后，在该目录执行：

```sh
docker compose -f docker-compose.prod.yaml pull
mkdir -p data
sudo chown 10001:10001 data
docker compose -f docker-compose.prod.yaml run --rm --no-deps wotm -config /data/config.yaml -init
docker compose -f docker-compose.prod.yaml up -d
```

Windows 用户将创建目录的两条命令替换为 `New-Item -ItemType Directory -Force data`，不需要执行 `chown`。

打开 `http://服务器地址:8080`，使用 `data/config.yaml` 中的 `web.username` 和 `web.password` 登录。

## 使用

1. **添加密钥**：在「SSH 密钥」中生成 Ed25519 密钥或上传未加密的私钥，复制公钥并手动添加到服务器的 `~/.ssh/authorized_keys`。
2. **添加主机**：填写地址、SSH 端口和用户名，选择使用密钥，或在连接时输入密码。
3. **连接终端**：点击主机的「连接」，首次连接需核对并确认服务器指纹。支持同时打开多个终端标签。
4. **上传文件**：连接后点击标签栏右侧的「上传文件」，或直接拖拽文件到终端，确认目标目录后开始上传。服务器需启用 SFTP，单文件最多 256 MiB，同名文件不会覆盖。上传目录不会随终端中的 `cd` 自动变化。

## 配置与备份

主机、SSH 私钥和 Web 登录密码保存在 `data/config.yaml` 中，可通过 Web 或手动编辑维护；手动修改后会自动加载。备份该文件即可保留全部配置。

Docker 需要挂载整个 `data` 目录，不能只挂载配置文件。私钥和 Web 密码按明文保存，请妥善保管该文件。

## 更新与停止

```sh
# 更新镜像并重启
docker compose -f docker-compose.prod.yaml pull
docker compose -f docker-compose.prod.yaml up -d

# 查看日志
docker compose -f docker-compose.prod.yaml logs -f

# 停止服务
docker compose -f docker-compose.prod.yaml down
```

## GHCR 镜像

推送到仓库的 `main` 或 `master` 分支后，Workflow 自动发布 amd64 / arm64 镜像到 `ghcr.io/vhvy/wotm`，提供 `latest` 和 `日期-commit hash` 标签，例如 `20261104-a1b2c3d4`。

生产部署默认使用 `latest` 和端口 `8080`。如需指定版本或端口，在部署目录创建 `.env`：

```dotenv
WOTM_TAG=20261104-a1b2c3d4
WOTM_PORT=8080
```

如果镜像为私有，需要先执行 `docker login ghcr.io`。

从源码构建时，使用项目中的 `compose.yaml`：`docker compose up -d --build`，首次启动前同样需要创建数据目录并初始化配置。
