#!/usr/bin/env bash
set -euo pipefail

REPO_URL="https://github.com/dimatolpygin/portfolio_bot_platform.git"
APP_DIR="/opt/bots-platform"
DOMAIN="anastasia-kwork.ru"
EMAIL="barkinhoevh3@gmail.com"
KEY_PATH="/root/.ssh/github_deploy"

info()    { echo "[INFO]  $*"; }
success() { echo "[OK]    $*"; }
warning() { echo "[WARN]  $*"; }

echo "========================================"
echo "  Bots Platform - VPS Setup (Ubuntu 24.04)"
echo "========================================"
echo ""

# 1. System update
info "Обновляю систему..."
apt-get update -qq
DEBIAN_FRONTEND=noninteractive apt-get upgrade -y -qq
apt-get install -y -qq curl gnupg ca-certificates lsb-release git
success "Система обновлена"

# 2. Docker Engine
info "Устанавливаю Docker Engine..."
if command -v docker &>/dev/null; then
    warning "Docker уже установлен: $(docker --version)"
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
    success "Docker установлен"
fi

# 3. Nginx + Certbot
info "Устанавливаю nginx и certbot..."
apt-get install -y -qq nginx certbot python3-certbot-nginx
systemctl enable --now nginx
success "Nginx установлен"

# 4. SSH-ключ для GitHub Actions
info "Генерирую SSH-ключ для GitHub Actions..."
mkdir -p /root/.ssh
chmod 700 /root/.ssh
if [ ! -f "${KEY_PATH}" ]; then
    ssh-keygen -t ed25519 -f "${KEY_PATH}" -N "" -C "github-actions-deploy"
    success "Ключ создан: ${KEY_PATH}"
else
    warning "Ключ уже существует: ${KEY_PATH}"
fi
if ! grep -qF "$(cat "${KEY_PATH}.pub")" /root/.ssh/authorized_keys 2>/dev/null; then
    cat "${KEY_PATH}.pub" >> /root/.ssh/authorized_keys
    chmod 600 /root/.ssh/authorized_keys
fi

# 5. Клонирование репозитория
info "Клонирую репозиторий в ${APP_DIR}..."
if [ -d "${APP_DIR}/.git" ]; then
    warning "Репозиторий уже существует - делаю git pull..."
    git -C "${APP_DIR}" pull --ff-only
else
    git clone "${REPO_URL}" "${APP_DIR}"
    success "Репозиторий склонирован"
fi

# 6. Создание .env
if [ ! -f "${APP_DIR}/.env" ]; then
    cp "${APP_DIR}/.env.example" "${APP_DIR}/.env"
    success "Создан ${APP_DIR}/.env"
else
    warning "${APP_DIR}/.env уже существует - пропускаю"
fi

# 7. Nginx конфиг
info "Настраиваю nginx для ${DOMAIN}..."
cp "${APP_DIR}/nginx/anastasia-kwork.ru.conf" \
   "/etc/nginx/sites-available/${DOMAIN}"
ln -sf \
   "/etc/nginx/sites-available/${DOMAIN}" \
   "/etc/nginx/sites-enabled/${DOMAIN}"
rm -f /etc/nginx/sites-enabled/default
nginx -t
systemctl reload nginx
success "Nginx настроен"

# 8. SSL-сертификат
info "Получаю SSL-сертификат Let's Encrypt для ${DOMAIN}..."
if [ -d "/etc/letsencrypt/live/${DOMAIN}" ]; then
    warning "Сертификат уже существует - пропускаю"
else
    certbot --nginx \
        -d "${DOMAIN}" -d "www.${DOMAIN}" \
        --non-interactive --agree-tos \
        -m "${EMAIL}" \
        --redirect
    success "SSL-сертификат получен"
fi
systemctl enable certbot.timer
systemctl start certbot.timer

# 9. Сделать скрипты исполняемыми
chmod +x "${APP_DIR}/scripts/"*.sh

# 10. Вывод инструкций
VPS_IP=$(curl -s --max-time 5 ifconfig.me 2>/dev/null || hostname -I | awk '{print $1}')

echo ""
echo "========================================"
echo "  УСТАНОВКА ЗАВЕРШЕНА!"
echo "========================================"
echo ""
echo "ШАГ 1: Добавь в GitHub -> Settings -> Secrets and variables -> Actions"
echo ""
printf "  %-16s = %s\n" "VPS_HOST"      "${VPS_IP}"
printf "  %-16s = %s\n" "VPS_USER"      "root"
printf "  %-16s = %s\n" "VPS_APP_DIR"   "${APP_DIR}"
printf "  %-16s = %s\n" "GHCR_USERNAME" "dimatolpygin"
printf "  %-16s = %s\n" "GHCR_PAT"      "<GitHub PAT: packages:write + repo>"
echo ""
echo "  VPS_SSH_KEY = (скопируй весь приватный ключ ниже):"
echo "  --------------------------------------------------"
cat "${KEY_PATH}"
echo "  --------------------------------------------------"
echo ""
echo "ШАГ 2: Заполни токены ботов"
echo "  nano ${APP_DIR}/.env"
echo ""
echo "ШАГ 3: Запусти платформу"
echo "  cd ${APP_DIR} && docker compose pull && docker compose up -d"
echo ""
echo "ШАГ 4: Проверь"
echo "  curl https://${DOMAIN}/healthz"
echo ""
