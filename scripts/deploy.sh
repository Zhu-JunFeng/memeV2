#!/usr/bin/env bash

set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

HOST="47.251.140.83"
SSH_USER="root"
SSH_PORT="22"
IDENTITY_FILE=""
REMOTE_DIR="/data/solana-scalper-v2"
SERVICE_NAME="solana-meme-backtest-v2.service"
BACKEND_PORT="8890"
GOARCH="amd64"
BASE_URL=""
SKIP_TESTS=false
ALLOW_ACTIVE_TRADES=false

usage() {
  cat <<'EOF'
Solana Scalper V2 生产部署脚本

用法：
  ./scripts/deploy.sh [选项]

选项：
  --host <地址>                 目标服务器，默认 47.251.140.83
  --user <用户>                 SSH 用户，默认 root
  --port <端口>                 SSH 端口，默认 22
  --identity <私钥路径>         SSH 私钥；不指定时使用 SSH 默认配置或交互密码
  --remote-dir <目录>           远端项目目录，默认 /data/solana-scalper-v2
  --service <服务名>            systemd 服务名，默认 solana-meme-backtest-v2.service
  --backend-port <端口>         远端后端监听端口，默认 8890
  --goarch <架构>               后端目标架构，默认 amd64，可指定 arm64
  --base-url <URL>              部署前后检查地址，默认 http://<host>
  --skip-tests                  跳过后端测试、vet 和前端测试，仅执行构建与部署
  --allow-active-trades         即使存在未完成订单或 open position 也继续部署
  -h, --help                    显示帮助

示例：
  ./scripts/deploy.sh
  ./scripts/deploy.sh --host 10.0.0.8 --user admin
  ./scripts/deploy.sh --host example.com --port 2222 --identity ~/.ssh/prod
EOF
}

require_value() {
  if [[ $# -lt 2 || -z "$2" ]]; then
    echo "参数 $1 缺少值" >&2
    exit 2
  fi
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --host)
      require_value "$@"
      HOST="$2"
      shift 2
      ;;
    --user)
      require_value "$@"
      SSH_USER="$2"
      shift 2
      ;;
    --port)
      require_value "$@"
      SSH_PORT="$2"
      shift 2
      ;;
    --identity)
      require_value "$@"
      IDENTITY_FILE="$2"
      shift 2
      ;;
    --remote-dir)
      require_value "$@"
      REMOTE_DIR="$2"
      shift 2
      ;;
    --service)
      require_value "$@"
      SERVICE_NAME="$2"
      shift 2
      ;;
    --backend-port)
      require_value "$@"
      BACKEND_PORT="$2"
      shift 2
      ;;
    --goarch)
      require_value "$@"
      GOARCH="$2"
      shift 2
      ;;
    --base-url)
      require_value "$@"
      BASE_URL="${2%/}"
      shift 2
      ;;
    --skip-tests)
      SKIP_TESTS=true
      shift
      ;;
    --allow-active-trades)
      ALLOW_ACTIVE_TRADES=true
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "未知参数：$1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

if [[ -z "$BASE_URL" ]]; then
  BASE_URL="http://$HOST"
fi

for command in curl jq ssh scp go npm; do
  if ! command -v "$command" >/dev/null 2>&1; then
    echo "缺少命令：$command" >&2
    exit 1
  fi
done

if [[ -n "$IDENTITY_FILE" && ! -f "$IDENTITY_FILE" ]]; then
  echo "SSH 私钥不存在：$IDENTITY_FILE" >&2
  exit 1
fi

CONTROL_PATH="${TMPDIR:-/tmp}/scalper-deploy-$$"
SSH_OPTIONS=(-p "$SSH_PORT" -o ControlMaster=auto -o ControlPersist=60 -o "ControlPath=$CONTROL_PATH")
SCP_OPTIONS=(-P "$SSH_PORT" -o ControlMaster=auto -o ControlPersist=60 -o "ControlPath=$CONTROL_PATH")
if [[ -n "$IDENTITY_FILE" ]]; then
  SSH_OPTIONS+=(-i "$IDENTITY_FILE")
  SCP_OPTIONS+=(-i "$IDENTITY_FILE")
fi

TARGET="$SSH_USER@$HOST"
RELEASE="$(date +%Y%m%d%H%M%S)-$$"
BUILD_DIR="$PROJECT_ROOT/.tmp/deploy/$RELEASE"
BACKEND_BINARY="$BUILD_DIR/solana-meme-backtest"
FRONTEND_DIST="$PROJECT_ROOT/frontend/dist"

echo "部署目标：$TARGET"
echo "远端目录：$REMOTE_DIR"
echo "服务名称：$SERVICE_NAME"
echo "目标架构：linux/$GOARCH"
echo "Release：$RELEASE"

if [[ "$SKIP_TESTS" == false ]]; then
  echo "[1/7] 执行后端测试和 vet"
  mkdir -p "$PROJECT_ROOT/backend/.tmp/go-build-cache"
  (
    cd "$PROJECT_ROOT/backend"
    GOCACHE="$PWD/.tmp/go-build-cache" go test ./...
    GOCACHE="$PWD/.tmp/go-build-cache" go vet ./...
  )

  echo "[2/7] 执行前端测试"
  (cd "$PROJECT_ROOT/frontend" && npm test)
else
  echo "[1-2/7] 已按参数跳过测试"
fi

echo "[3/7] 构建 Linux 后端和前端"
mkdir -p "$BUILD_DIR"
(
  cd "$PROJECT_ROOT/backend"
  CGO_ENABLED=0 GOOS=linux GOARCH="$GOARCH" \
    go build -trimpath -o "$BACKEND_BINARY" ./cmd/server
)
(cd "$PROJECT_ROOT/frontend" && npm run build)

echo "[4/7] 检查目标服务交易状态"
orders_json="$(curl --fail --silent --show-error "$BASE_URL/api/trade/orders?limit=200")"
positions_json="$(curl --fail --silent --show-error "$BASE_URL/api/trade/positions?status=open&limit=200")"
active_order_count="$(jq '[.data.items[] | select(.status == "pending" or .status == "submitted")] | length' <<<"$orders_json")"
open_position_count="$(jq '.data.items | length' <<<"$positions_json")"
echo "未完成订单：$active_order_count"
echo "Open positions：$open_position_count"
if [[ "$ALLOW_ACTIVE_TRADES" == false && ("$active_order_count" != "0" || "$open_position_count" != "0") ]]; then
  echo "检测到活跃交易，已停止部署。确认可重启后使用 --allow-active-trades。" >&2
  exit 1
fi

echo "[5/7] 创建远端 release 目录并上传文件"
ssh "${SSH_OPTIONS[@]}" "$TARGET" bash -s -- "$REMOTE_DIR" "$RELEASE" <<'REMOTE_PREPARE'
set -Eeuo pipefail
remote_dir="$1"
release="$2"
mkdir -p "$remote_dir/backend" "$remote_dir/frontend.release-$release"
REMOTE_PREPARE

scp "${SCP_OPTIONS[@]}" "$BACKEND_BINARY" "$TARGET:$REMOTE_DIR/backend/solana-meme-backtest.new-$RELEASE"
scp "${SCP_OPTIONS[@]}" -r "$FRONTEND_DIST/." "$TARGET:$REMOTE_DIR/frontend.release-$RELEASE/"

echo "[6/7] 备份并切换远端版本"
ssh "${SSH_OPTIONS[@]}" "$TARGET" bash -s -- "$REMOTE_DIR" "$SERVICE_NAME" "$BACKEND_PORT" "$RELEASE" <<'REMOTE_DEPLOY'
set -Eeuo pipefail
remote_dir="$1"
service_name="$2"
backend_port="$3"
release="$4"

cd "$remote_dir"
if [[ -f backend/solana-meme-backtest ]]; then
  cp backend/solana-meme-backtest "backend/solana-meme-backtest.bak-$release"
fi
chmod 755 "backend/solana-meme-backtest.new-$release"
mv -f "backend/solana-meme-backtest.new-$release" backend/solana-meme-backtest

if [[ -d frontend ]]; then
  mv frontend "frontend.bak-$release"
fi
mv "frontend.release-$release" frontend

systemctl restart "$service_name"
sleep 4
systemctl is-active "$service_name"
systemctl show "$service_name" -p MainPID -p NRestarts -p ActiveEnterTimestamp
curl --fail --silent --show-error "http://127.0.0.1:$backend_port/api/health"
echo
REMOTE_DEPLOY

echo "[7/7] 验证公网健康状态"
curl --fail --silent --show-error "$BASE_URL/api/health" | jq '.data'

echo "部署完成：$BASE_URL"
echo "后端备份：$REMOTE_DIR/backend/solana-meme-backtest.bak-$RELEASE"
echo "前端备份：$REMOTE_DIR/frontend.bak-$RELEASE"
