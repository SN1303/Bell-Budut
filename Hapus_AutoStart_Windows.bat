@echo off
title Hapus Auto-Start - Sistem Bel Sekolah Budut
echo ==========================================================
echo        MENGHAPUS AUTO-START SISTEM BEL SEKOLAH
echo ==========================================================
echo.
echo Menghapus dari Windows Startup Registry...
reg delete "HKCU\Software\Microsoft\Windows\CurrentVersion\Run" /v "BellBudut" /f >nul 2>&1

echo.
echo [SELESAI] Auto-start berhasil dimatikan.
echo Aplikasi tidak akan lagi berjalan otomatis saat komputer dinyalakan.
echo Anda tetap bisa menjalankannya manual via Jalankan_Bel.bat
echo.
echo ==========================================================
pause

