#!/usr/bin/env bash
set -euo pipefail

REPO_URL="https://github.com/dimatolpygin/portfolio_bot_platform.git"
APP_DIR="/opt/bots-platform"
DOMAIN="anastasia-kwork.ru"
EMAIL="barkinhoevh3@gmail.com"
KEY_PATH="/root/.ssh/github_deploy"

GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m'

log()  { echo -e "${CYAN}→${NC} $*"; }
done() { echo -e "${GREEN}✓${NC} $*"; }
warn() { echo -e "${YELLOW}⚠${NC} $*"; }

echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}  Bots Platform — VPS Setup (Ubuntu 24.04)${NC}"
echo -e "${GREEN}========================================${NC}"
echo ""

# ── 1. System update ────────────────────────────────────────────────────────
log "Обновляю систему..."
apt-get update -qq
DEBIAN_FRONTEND=noninteractive apt-get upgrade -y -qq
apt-get install -y -qq curl gnupg ca-certificates lsb-release git
done "Система обновлена"

# ── 2. Docker Engine ────────────────────────────────────────────────────────
log "Устанавливаю Docker Engine..."
if command -v docker &>/dev/null; then
    warn "Docker уже установлен: $(docker --version)"
else
    install -m 0755 -d /etc/apt/keyrings
    curl -fsSL https://download.docker.com/linux/ubuntu/gpg \
        | gpg --dearmor -o /etc/apt/keyrings/docker.gpg
    chmod a+r /etc/apt/keyrings/docker.gpg
    echo \
        "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] \
        https://download.docker.com/linux/ubuntu \
        $(. /etc/os-release && echo "${VERSION_CODENAME}") stable" \
        | tee /etc/apt/sources.list.d/docker.list > /dev/null
    apt-get update -qq
    apt-get install -y -qq \
        docker-ce docker-ce-cli containerd.io \
        docker-buildx-plugin docker-compose-plugin
    systemctl enable --now docker
    done "Docker $(docker --version | grep -oP '[\d.]+'  | head -1) установлен"
fi

# ── 3. Nginx + Certbot ──────────────────────────────────────────────────────
log "Устанавливаю nginx и certbot..."
apt-get install -y -qq nginx certbot python3-certbot-nginx
systemctl enable --now nginx
done "Nginx $(nginx -v 2>&1 | grep -oP '[\d.]+') установлен"

# ── 4. SSH-ключ для GitHub Actions ─────────────────────────────────────────
log "Генерирую SSH-ключ для GitHub Actions..."
mkdir -p /root/.ssh
chmod 700 /root/.ssh
if [ ! -f "${KEY_PATH}" ]; then
    ssh-keygen -t ed25519 -f "${KEY_PATH}" -N "" -C "github-actions-deploy"
    done "Ключ создан: ${KEY_PATH}"
else
    warn "Ключ уже существует: ${KEY_PATH}"
fi
if ! grep -qF "$(cat "${KEY_PATH}.pub")" /root/.ssh/authorized_keys 2>/dev/null; then
    cat "${KEY_PATH}.pub" >> /root/.ssh/authorized_keys
    chmod 600 /root/.ssh/authorized_keys
fi

# ── 5. Клонирование репозитория ─────────────────────────────────────────────
log "Клонирую репозиторий в ${APP_DIR}..."
if [ -d "${APP_DIR}/.git" ]; then
    warn "Репозиторий уже существует — делаю git pull..."
    git -C "${APP_DIR}" pull --ff-only
else
    git clone "${REPO_URL}" "${APP_DIR}"
    done "Репозиторий склонирован"
fi

# ── 6. Создание .env ────────────────────────────────────────────────────────
if [ ! -f "${APP_DIR}/.env" ]; then
    cp "${APP_DIR}/.env.example" "${APP_DIR}/.env"
    done "Создан ${APP_DIR}/.env — заполни токены ботов!"
else
    warn "${APP_DIR}/.env уже существует — пропускаю"
fi

# ── 7. Nginx конфиг ─────────────────────────────────────────────────────────
log "Настраиваю nginx для ${DOMAIN}..."
cp "${APP_DIR}/nginx/anastasia-kwork.ru.conf" \
   "/etc/nginx/sites-available/${DOMAIN}"
ln -sf \
   "/etc/nginx/sites-available/${DOMAIN}" \
   "/etc/nginx/sites-enabled/${DOMAIN}"
rm -f /etc/nginx/sites-enabled/default
nginx -t
systemctl reload nginx
done "Nginx настроен"

# ── 8. SSL-сертификат ───────────────────────────────────────────────────────
log "Получаю SSL-сертификат Let's Encrypt для ${DOMAIN}..."
if [ -d "/etc/letsencrypt/live/${DOMAIN}" ]; then
    warn "Сертификат уже существует — пропускаю"
else
    certbot --nginx \
        -d "${DOMAIN}" -d "www.${DOMAIN}" \
        --non-interactive --agree-tos \
        -m "${EMAIL}" \
        --redirect
    done "SSL-сертификат получен"
fi
systemctl enable certbot.timer
systemctl start certbot.timer

# ── 9. Сделать скрипты исполняемыми ─────────────────────────────────────────
chmod +x "${APP_DIR}/scripts/"*.sh

# ── 10. Вывод инструкций ────────────────────────────────────────────────────
VPS_IP=$(curl -s --max-time 5 ifconfig.me 2>/dev/null || hostname -I | awk '{print $1}')

echo ""
echo -e "${GREEN}════════════════════════════════════════════════════════════${NC}"
echo -e "${GREEN}  УСТАНОВКА ЗАВЕРШЕНА!${NC}"
echo -e "${GREEN}════════════════════════════════════════════════════════════${NC}"
echo ""
echo -e "${YELLOW}ШАГ 1: Добавь в GitHub → Settings → Secrets and variables → Actions${NC}"
echo ""
printf "  %-16s = %s\n" "VPS_HOST"      "${VPS_IP}"
printf "  %-16s = %s\n" "VPS_USER"      "root"
printf "  %-16s = %s\n" "VPS_APP_DIR"   "${APP_DIR}"
printf "  %-16s = %s\n" "GHCR_USERNAME" "dimatolpygin"
printf "  %-16s = %s\n" "GHCR_PAT"      "<GitHub PAT: packages:write + repo>"
echo ""
echo "  VPS_SSH_KEY = (приватный ключ ниже):"
echo "  ─────────────────────────────────────"
cat "${KEY_PATH}"
echo "  ─────────────────────────────────────"
echo ""
echo -e "${YELLOW}ШАГ 2: Заполни токены ботов в ${APP_DIR}/.env${NC}"
echo "  nano ${APP_DIR}/.env"
echo ""
echo -e "${YELLOW}ШАГ 3: Запусти платформу${NC}"
echo "  cd ${APP_DIR} && docker compose pull && docker compose up -d"
echo ""
echo -e "${YELLOW}ШАГ 4: Проверь${NC}"
echo "  curl https://${DOMAIN}/healthz"
echo ""
