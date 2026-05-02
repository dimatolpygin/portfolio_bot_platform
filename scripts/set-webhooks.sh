#!/usr/bin/env bash
set -euo pipefail

APP_DIR="${1:-/opt/bots-platform}"
DOMAIN="anastasia-kwork.ru"
ENV_FILE="${APP_DIR}/.env"
BOTS_YML="${APP_DIR}/config/bots.yml"

if [ ! -f "${ENV_FILE}" ]; then
    echo "Ошибка: .env не найден в ${APP_DIR}" >&2
    exit 1
fi

if [ ! -f "${BOTS_YML}" ]; then
    echo "Ошибка: config/bots.yml не найден в ${APP_DIR}" >&2
    exit 1
fi

# Загружаем переменные из .env (игнорируем комментарии и пустые строки)
set -a
# shellcheck disable=SC1090
source <(grep -v '^\s*#' "${ENV_FILE}" | grep -v '^\s*$')
set +a

# Извлекаем слаги ботов из bots.yml
SLUGS=$(grep -E '^\s+- slug:\s+' "${BOTS_YML}" | awk '{print $3}')

if [ -z "${SLUGS}" ]; then
    echo "Ботов не найдено в config/bots.yml" >&2
    exit 1
fi

SUCCESS=0
SKIPPED=0
FAILED=0

echo "Регистрирую вебхуки для домена https://${DOMAIN}..."
echo ""

for SLUG in ${SLUGS}; do
    # sample-echo → BOT_SAMPLE_ECHO_TOKEN
    SUFFIX=$(echo "${SLUG}" | tr '[:lower:]-' '[:upper:]_')
    TOKEN_VAR="BOT_${SUFFIX}_TOKEN"
    SECRET_VAR="BOT_${SUFFIX}_WEBHOOK_SECRET"

    TOKEN="${!TOKEN_VAR:-}"
    SECRET="${!SECRET_VAR:-}"

    if [ -z "${TOKEN}" ] || [[ "${TOKEN}" == *"replace-me"* ]]; then
        echo "  ⚠  ${SLUG}: ${TOKEN_VAR} не задан — пропускаю"
        SKIPPED=$((SKIPPED + 1))
        continue
    fi

    WEBHOOK_URL="https://${DOMAIN}/telegram/${SLUG}/webhook"

    PAYLOAD="{\"url\":\"${WEBHOOK_URL}\""
    if [ -n "${SECRET}" ] && [[ "${SECRET}" != *"replace-with"* ]]; then
        PAYLOAD="${PAYLOAD},\"secret_token\":\"${SECRET}\""
    fi
    PAYLOAD="${PAYLOAD}}"

    RESPONSE=$(curl -s --max-time 10 \
        -X POST "https://api.telegram.org/bot${TOKEN}/setWebhook" \
        -H "Content-Type: application/json" \
        -d "${PAYLOAD}")

    if echo "${RESPONSE}" | grep -q '"ok":true'; then
        echo "  ✓  ${SLUG} → ${WEBHOOK_URL}"
        SUCCESS=$((SUCCESS + 1))
    else
        DESCRIPTION=$(echo "${RESPONSE}" | grep -oP '"description":"\K[^"]+' || echo "неизвестная ошибка")
        echo "  ✗  ${SLUG}: ${DESCRIPTION}"
        FAILED=$((FAILED + 1))
    fi
done

echo ""
echo "Итого: ✓ ${SUCCESS} успешно | ⚠ ${SKIPPED} пропущено | ✗ ${FAILED} ошибок"

if [ "${FAILED}" -gt 0 ]; then
    exit 1
fi
