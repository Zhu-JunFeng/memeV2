set -e
mkdir -p /data/solana-scalper-v2/{backend,frontend,data,logs}
cat > /etc/systemd/system/solana-meme-backtest-v2.service <<'SERVICE'
[Unit]
Description=Solana Meme Backtest V2
After=network.target

[Service]
Type=simple
WorkingDirectory=/data/solana-scalper-v2/backend
ExecStart=/data/solana-scalper-v2/backend/solana-meme-backtest
Restart=always
RestartSec=5
Environment=GIN_MODE=release
EnvironmentFile=-/data/solana-scalper-v2/backend/.env

[Install]
WantedBy=multi-user.target
SERVICE
python3 - <<'PY'
from pathlib import Path
path = Path('/etc/nginx/conf.d/typing-race.conf')
text = path.read_text()
old = 'server_name keyflow.zcn.world 182.92.160.46 182-92-160-46.nip.io;'
new = 'server_name keyflow.zcn.world 182-92-160-46.nip.io;'
if old in text:
    path.write_text(text.replace(old, new))
PY
cat > /etc/nginx/conf.d/solana-meme-backtest-v2.conf <<'NGINX'
server {
    listen 80;
    server_name 182.92.160.46;

    root /data/solana-scalper-v2/frontend;
    index index.html;

    location /api/ {
        proxy_pass http://127.0.0.1:8890/api/;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_read_timeout 300s;
        proxy_connect_timeout 30s;
        client_max_body_size 10m;
    }

    location = /index.html {
        add_header Cache-Control "no-store, no-cache, must-revalidate";
    }

    location / {
        try_files $uri $uri/ /index.html;
    }
}
NGINX
systemctl daemon-reload
systemctl enable solana-meme-backtest-v2.service >/dev/null
systemctl restart solana-meme-backtest-v2.service
nginx -t
systemctl reload nginx
systemctl --no-pager --full status solana-meme-backtest-v2.service | sed -n '1,80p'
