@echo off
title Pasang Auto-Start - Sistem Bel Sekolah Budut
echo ==========================================================
echo       MEMASANG AUTO-START SISTEM BEL SEKOLAH BUDUT
echo ==========================================================
echo.
cd /d "%~dp0"

echo Mendaftarkan ke Windows Startup Registry...
reg add "HKCU\Software\Microsoft\Windows\CurrentVersion\Run" /v "BellBudut" /t REG_SZ /d "wscript.exe \"%~dp0Jalankan_Background.vbs\"" /f >nul

if %errorlevel% equ 0 (
    echo.
    echo [SUKSES] Aplikasi Bel Sekolah BERHASIL diatur untuk aktif otomatis!
    echo Begitu komputer/laptop dinyalakan dan masuk Windows, bel akan
    echo otomatis berjalan di latar belakang (background) tanpa jendela hitam.
    echo.
    echo Dashboard tetap dapat diakses di: http://localhost:8080
) else (
    echo.
    echo [GAGAL] Terjadi kesalahan saat mendaftarkan auto-start.
)

echo.
echo ==========================================================
pause

