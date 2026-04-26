@echo off
setlocal enabledelayedexpansion

:: ── AgentProxy uninstaller for Windows ────────────────────────────────────
:: Usage:
::   uninstall.bat              -- remove binary, certs, runtime dirs, and Root-store CA trust
::   uninstall.bat --keep-trust -- keep Root-store CA trust, remove local files and commands only

cd /d "%~dp0"

set KEEP_TRUST=
for %%A in (%*) do (
    if "%%A"=="--trust" rem Backward-compatible no-op: trust removal is now the default.
    if "%%A"=="--keep-trust" set KEEP_TRUST=yes
    if "%%A"=="--help" goto :usage
    if "%%A"=="-h" goto :usage
)

set CA_CERT=%USERPROFILE%\.agentproxy\certs\agentproxy-ca-cert.pem

:: ── Remove OS trust ──────────────────────────────────────────────────────────
if "%KEEP_TRUST%"=="yes" (
    echo Keeping Windows Root-store CA trust because --keep-trust was supplied.
) else (
    echo Removing CA cert from Windows Root store ...
    certutil -delstore "Root" "goproxy.github.io" 2>nul
    if errorlevel 1 (
        echo Certificate not found in Root store (may have been removed already^).
    ) else (
        echo Removed CA cert from Root store.
    )
)

:: ── Remove CA cert directory ─────────────────────────────────────────────────
set CERT_DIR=%USERPROFILE%\.agentproxy\certs
if exist "%CERT_DIR%" (
    rmdir /s /q "%CERT_DIR%"
    echo Removed %CERT_DIR%
)

:: ── Remove local runtime directories ────────────────────────────────────────
if exist certs\ (
    rmdir /s /q certs
    echo Removed .\certs
)

:: ── Remove binary ────────────────────────────────────────────────────────────
if exist agentproxy.exe (
    del /f /q agentproxy.exe
    echo Removed agentproxy.exe
)

:: ── Remove installed commands from %USERPROFILE%\.local\bin ──────────────────
set INSTALL_DIR=%USERPROFILE%\.local\bin
for %%F in (agentproxy.exe claudeproxy.bat codexproxy.bat copilotproxy.bat agentproxy-start.bat) do (
    if exist "%INSTALL_DIR%\%%F" (
        del /f /q "%INSTALL_DIR%\%%F"
        echo Removed %INSTALL_DIR%\%%F
    )
)

echo.
echo AgentProxy uninstalled. The .\logs directory was left in place.
echo.
pause
exit /b 0

:usage
echo Usage: uninstall.bat [--keep-trust]
echo Default: remove installed commands, local cert files, runtime cert cache, and Windows Root-store CA trust.
echo   --keep-trust   Skip Root-store trust removal.
exit /b 0
