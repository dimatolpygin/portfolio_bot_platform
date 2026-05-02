param(
    [string]$TunnelUrl
)

$ErrorActionPreference = "Stop"
$ScriptDir = $PSScriptRoot

$envFile = Join-Path $ScriptDir ".env"
$envVars = @{}
foreach ($line in Get-Content $envFile) {
    if ($line -match '^\s*#' -or $line -notmatch '=') { continue }
    $parts = $line -split '=', 2
    $envVars[$parts[0].Trim()] = $parts[1].Trim()
}

if (-not $TunnelUrl) {
    Write-Host "No tunnel URL provided. Start cloudflared manually and pass URL as argument." -ForegroundColor Red
    exit 1
}

$botsConfig = Join-Path $ScriptDir "config\bots.yml"
$slugs = Select-String -Path $botsConfig -Pattern '^\s*-\s*slug:\s*(.+)' |
    ForEach-Object { $_.Matches[0].Groups[1].Value.Trim() }

if ($slugs.Count -eq 0) {
    Write-Error "No bots found in $botsConfig"
    exit 1
}

foreach ($slug in $slugs) {
    $envKey    = $slug.ToUpper() -replace '-', '_'
    $tokenKey  = "BOT_${envKey}_TOKEN"
    $secretKey = "BOT_${envKey}_WEBHOOK_SECRET"

    $token  = $envVars[$tokenKey]
    $secret = $envVars[$secretKey]

    if (-not $token) {
        Write-Warning "[$slug] skipped: $tokenKey not found in .env"
        continue
    }

    $webhookUrl = "$TunnelUrl/telegram/$slug/webhook"
    $apiUrl     = "https://api.telegram.org/bot$token/setWebhook"

    $body = "url=$webhookUrl"
    if ($secret) { $body += "&secret_token=$secret" }

    try {
        $resp = Invoke-RestMethod -Method Post -Uri $apiUrl -Body $body -ContentType "application/x-www-form-urlencoded"
        if ($resp.ok) {
            Write-Host "[$slug] OK -> $webhookUrl" -ForegroundColor Green
        } else {
            Write-Warning "[$slug] Telegram error: $($resp.description)"
        }
    } catch {
        Write-Warning "[$slug] Request failed: $_"
    }
}
