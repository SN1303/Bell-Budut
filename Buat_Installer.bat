@echo off
title Membuat Installer - Sistem Bel Sekolah Budut
cd /d "%~dp0"
echo ==========================================================
echo   MEMBUAT INSTALLER (berisi jadwal, pengaturan ^& audio saat ini)
echo ==========================================================
echo.

if not exist dist mkdir dist

echo [1/3] Build aplikasi ...
go build -trimpath -ldflags "-s -w" -o dist\bell-budut.exe . || goto :fail

echo [2/3] Mengemas aplikasi + data saat ini ...
go run ./tools/bundle -exe dist\bell-budut.exe -out installer\payload.zip || goto :fail

echo [3/3] Build Setup.exe ...
go build -trimpath -ldflags "-s -w" -o dist\BellBudut-Setup.exe ./installer || goto :fail
del /q installer\payload.zip >nul 2>&1

echo.
echo [SUKSES] Installer siap: dist\BellBudut-Setup.exe
echo Salin file itu ke komputer sekolah lalu klik 2x.
echo PERHATIAN: file ini berisi data sekolah, JANGAN diunggah ke GitHub.
echo.
pause
exit /b 0

:fail
echo.
echo [GAGAL] Pembuatan installer gagal.
pause
exit /b 1
