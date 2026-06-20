#!/bin/sh

DB_PATH="${SUI_DB_FOLDER:-/app/db}/s-ui.db"
if [ -f "$DB_PATH" ]; then
	./sui migrate
fi

# 预装 acme.sh（如果未安装）
if [ ! -f /root/.acme.sh/acme.sh ]; then
	curl -s https://get.acme.sh | sh -s email=admin@s-ui.local || true
fi

exec ./sui