@echo off
title Sistem Bel Sekolah Budut
echo ==========================================================
echo        MENJALANKAN SISTEM BEL SEKOLAH BUDUT
echo ==========================================================
echo.
cd /d "%~dp0"
if exist "bell-budut.exe" (
    start http://localhost:8080
    bell-budut.exe -port 8080
) else (
    echo [ERROR] File bell-budut.exe tidak ditemukan.
    pause
)

