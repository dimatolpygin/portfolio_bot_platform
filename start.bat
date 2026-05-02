@echo off
chcp 65001 > nul
cd /d %~dp0

for /f "usebackq tokens=1,* delims==" %%A in (".env") do (
    if not "%%A"=="" if not "%%A:~0,1%"=="#" set "%%A=%%B"
)

go run ./cmd/server -mode=all
pause
