#!/usr/bin/env bash
# Installs or upgrades the PPanel server on a Linux host with systemd.
# 在带 systemd 的 Linux 主机上安装或升级 PPanel 服务端。
#
#   sudo bash install.sh
#
# Downloads the latest release archive for this machine (linux amd64 or arm64)
# from github.com/lolitia-connect/backend, verifies it against the release's
# per-asset .sha256 file, installs it to /opt/ppanel-server owned by the system
# user "ppanel", writes the systemd unit "ppanel.service" and starts it.
# Running it again upgrades the binary and keeps config.yaml.
#
# Environment / 环境变量:
#   PPANEL_VERSION   release tag to install, e.g. v1.3.9 (default: latest)
#   PPANEL_DB        with PPANEL_REDIS: written to config.env so the first
#   PPANEL_REDIS     start completes the configuration without the wizard
#                    (formats: doc/config.md, section 5)
set -euo pipefail

# This fork publishes its releases here. Upstream is perfect-panel/server.
REPO="lolitia-connect/backend"
INSTALL_DIR="/opt/ppanel-server"
SERVICE_NAME="ppanel"
SERVICE_USER="ppanel"
UNIT_FILE="/etc/systemd/system/${SERVICE_NAME}.service"
# The fork's configuration file is config.yaml in the working directory
# (upstream uses etc/ppanel.yaml).
CONFIG_FILE="${INSTALL_DIR}/config.yaml"
ENV_FILE="${INSTALL_DIR}/config.env"
# The subcommand name. The fork's docs and service file start the server as
# "run"; the binary also accepts --config.
CONFIG_FLAG="--config ${CONFIG_FILE}"
VERSION="${PPANEL_VERSION:-}"

msg() { printf '==> %s\n' "$*"; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }

# --- preflight / 环境检查 ---------------------------------------------------

[ "$(id -u)" -eq 0 ] || die "run this script as root / 请以 root 用户运行此脚本"
[ "$(uname -s)" = "Linux" ] || die "Linux only; other systems: unpack the release archive and run ppanel-server run --config config.yaml / 仅支持 Linux"
command -v systemctl >/dev/null 2>&1 || die "systemd is required / 需要 systemd"
for cmd in curl tar sha256sum install; do
	command -v "$cmd" >/dev/null 2>&1 || die "$cmd is required / 需要安装 $cmd"
done

case "$(uname -m)" in
x86_64 | amd64) ARCH="amd64" ;;
aarch64 | arm64) ARCH="arm64" ;;
*) die "unsupported architecture $(uname -m): release archives exist for amd64 and arm64 / 不支持的架构，发布包只提供 amd64 与 arm64" ;;
esac

# --- resolve the release / 解析版本 -----------------------------------------

if [ -z "$VERSION" ]; then
	# The "latest" page redirects to /releases/tag/<tag>; no API token or jq needed.
	VERSION="$(curl -fsSL -o /dev/null -w '%{url_effective}' "https://github.com/${REPO}/releases/latest" | sed -n 's#.*/releases/tag/##p')"
	[ -n "$VERSION" ] || die "could not determine the latest release; set PPANEL_VERSION=vX.Y.Z / 无法获取最新版本号，请检查网络或设置 PPANEL_VERSION"
fi

ASSET="ppanel-server-linux-${ARCH}.tar.gz"
BASE_URL="https://github.com/${REPO}/releases/download/${VERSION}"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

# --- download and verify / 下载并校验 ---------------------------------------

msg "Downloading ${ASSET} ${VERSION} / 正在下载 ${ASSET} ${VERSION} ..."
curl -fsSL --retry 3 -o "${TMP}/${ASSET}" "${BASE_URL}/${ASSET}" || die "download failed: ${BASE_URL}/${ASSET} / 下载失败"

# The release publishes one "<archive>.sha256" next to each archive. There is no
# combined SHA256SUMS file; if that ever changes, this step still verifies the
# one file that was actually downloaded.
msg "Verifying the checksum / 校验文件完整性 ..."
curl -fsSL --retry 3 -o "${TMP}/${ASSET}.sha256" "${BASE_URL}/${ASSET}.sha256" \
	|| die "this release has no ${ASSET}.sha256; refusing to install an unverified binary. Verify it by hand or pick a newer release / 该版本没有 ${ASSET}.sha256，拒绝安装未校验的二进制"
[ -s "${TMP}/${ASSET}.sha256" ] || die "${ASSET}.sha256 is empty / 校验文件为空"
(cd "$TMP" && sha256sum -c "${ASSET}.sha256") || die "checksum mismatch: the download is corrupt or tampered with / 校验失败，下载的文件已损坏或被篡改"

mkdir -p "${TMP}/extract"
tar -xzf "${TMP}/${ASSET}" -C "${TMP}/extract"
BINARY="$(find "${TMP}/extract" -type f -name ppanel-server | head -n 1)"
[ -n "$BINARY" ] || die "ppanel-server not found in ${ASSET} / 压缩包中没有 ppanel-server"

# --- system user and files / 系统用户与文件 ---------------------------------

if ! id -u "$SERVICE_USER" >/dev/null 2>&1; then
	msg "Creating the system user ${SERVICE_USER} / 创建系统用户 ${SERVICE_USER} ..."
	NOLOGIN="$(command -v nologin || echo /usr/sbin/nologin)"
	useradd --system --home-dir "$INSTALL_DIR" --no-create-home --shell "$NOLOGIN" "$SERVICE_USER"
fi

if systemctl is-active --quiet "$SERVICE_NAME"; then
	msg "Stopping the running service for the upgrade / 停止正在运行的服务以便升级 ..."
	systemctl stop "$SERVICE_NAME"
fi

msg "Installing to ${INSTALL_DIR} / 安装到 ${INSTALL_DIR} ..."
mkdir -p "${INSTALL_DIR}"
install -m 0755 -o root -g root "$BINARY" "${INSTALL_DIR}/ppanel-server"
LICENSE_FILE="$(find "${TMP}/extract" -type f -name LICENSE | head -n 1)"
[ -z "$LICENSE_FILE" ] || install -m 0644 -o root -g root "$LICENSE_FILE" "${INSTALL_DIR}/LICENSE"
# The configuration holds the JWT secret and the database credentials once it is
# completed: owner-only, and never overwritten by an upgrade. The server itself
# creates a missing file with mode 0600 on start.
if [ ! -e "$CONFIG_FILE" ]; then
	install -m 0600 -o "$SERVICE_USER" -g "$SERVICE_USER" /dev/null "$CONFIG_FILE"
fi
if [ -n "${PPANEL_DB:-}" ] && [ -n "${PPANEL_REDIS:-}" ]; then
	msg "Writing PPANEL_DB and PPANEL_REDIS to config.env for the first start / 将 PPANEL_DB 与 PPANEL_REDIS 写入 config.env 供首次启动使用 ..."
	install -m 0600 -o "$SERVICE_USER" -g "$SERVICE_USER" /dev/null "$ENV_FILE"
	printf 'PPANEL_DB=%s\nPPANEL_REDIS=%s\n' "$PPANEL_DB" "$PPANEL_REDIS" > "$ENV_FILE"
elif [ -n "${PPANEL_DB:-}" ] || [ -n "${PPANEL_REDIS:-}" ]; then
	die "PPANEL_DB and PPANEL_REDIS must be set together / PPANEL_DB 与 PPANEL_REDIS 必须同时设置"
fi
# Logs and the GeoIP cache are written under the working directory and are
# bind-mounted as volumes by docker-compose.
mkdir -p "${INSTALL_DIR}/logs" "${INSTALL_DIR}/cache"
chown "$SERVICE_USER:$SERVICE_USER" "$INSTALL_DIR" "$INSTALL_DIR/logs" "$INSTALL_DIR/cache" "$CONFIG_FILE"
chmod 0750 "$INSTALL_DIR"

# --- systemd unit / systemd 服务 -------------------------------------------

msg "Writing ${UNIT_FILE} / 写入 systemd 服务文件 ..."
cat > "$UNIT_FILE" <<EOF
[Unit]
Description=PPanel Server
Documentation=https://github.com/${REPO}/blob/master/doc/install.md
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=${SERVICE_USER}
Group=${SERVICE_USER}
WorkingDirectory=${INSTALL_DIR}
# Optional: PPANEL_DB and PPANEL_REDIS for a non-interactive first start.
EnvironmentFile=-${ENV_FILE}
ExecStart=${INSTALL_DIR}/ppanel-server run ${CONFIG_FLAG}
Restart=on-failure
RestartSec=5s
# Shutdown drains HTTP, then the scheduler, the task worker and the trace
# exporter.
TimeoutStopSec=25
# The server writes only under its own directory.
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
ReadWritePaths=${INSTALL_DIR}

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable --quiet "$SERVICE_NAME"
msg "Starting ${SERVICE_NAME} / 启动服务 ..."
systemctl start "$SERVICE_NAME"
sleep 2
systemctl --no-pager --lines=0 status "$SERVICE_NAME" || true

# --- next steps / 后续步骤 ---------------------------------------------------

cat <<EOF

PPanel server ${VERSION} is installed in ${INSTALL_DIR} and runs as ${SERVICE_USER} (systemd unit: ${SERVICE_NAME}).
PPanel 服务端 ${VERSION} 已安装到 ${INSTALL_DIR}，以 ${SERVICE_USER} 用户运行（systemd 服务：${SERVICE_NAME}）。

EOF

if grep -q 'AccessSecret' "$CONFIG_FILE" 2>/dev/null; then
	cat <<EOF
The configuration is complete; the API listens on the Host:Port of config.yaml (0.0.0.0:8080 by default).
配置已完成，API 监听 config.yaml 中的 Host:Port（默认 0.0.0.0:8080）。
  journalctl -u ${SERVICE_NAME} -f          # logs; the first administrator's password is printed once / 日志，首位管理员密码只打印一次
  curl -fsS http://127.0.0.1:8080/readyz    # readiness / 就绪探针
EOF
else
	cat <<EOF
Finish the installation in ONE of two ways / 请用以下两种方式之一完成安装：

1. Setup wizard. It listens on 127.0.0.1 of this server only, at the configured Port (8080
   by default) and never on a public interface, so open an SSH tunnel from your computer and
   use the browser there (administrator password: at least 8 characters):
   安装向导只监听本机的 127.0.0.1（端口为配置的 Port，默认 8080，绝不监听公网），请在自己的电脑上
   建立 SSH 隧道后用浏览器访问（管理员密码至少 8 个字符）：
     ssh -L 8080:127.0.0.1:8080 root@<this-server>
     http://127.0.0.1:8080/init

2. Non-interactive. Write the connections to ${ENV_FILE} and restart;
   the server completes config.yaml, migrates the database and prints the first
   administrator's password once in the log:
   无人值守：把连接信息写入 ${ENV_FILE} 后重启，服务会补全 config.yaml、执行迁移，
   并在日志中打印一次首位管理员的密码：
     install -m 0600 -o ${SERVICE_USER} -g ${SERVICE_USER} /dev/null ${ENV_FILE}
     printf 'PPANEL_DB=%s\nPPANEL_REDIS=%s\n' 'user:password@tcp(127.0.0.1:3306)/ppanel' 'redis://:password@127.0.0.1:6379/0' > ${ENV_FILE}
     systemctl restart ${SERVICE_NAME} && journalctl -u ${SERVICE_NAME} -f
   PostgreSQL: PPANEL_DB=postgres://user:password@127.0.0.1:5432/ppanel?sslmode=require

Documentation / 文档: https://github.com/${REPO}/blob/master/doc/install.md
EOF
fi
