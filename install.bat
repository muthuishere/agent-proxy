@echo off
setlocal enabledelayedexpansion

:: ── AgentProxy installer for Windows ──────────────────────────────────────
:: Usage: double-click install.bat  or  run from Command Prompt / PowerShell

cd /d "%~dp0"

:: ── Check Python ────────────────────────────────────────────────────────────
set PYTHON=

for %%P in (python python3) do (
    if "!PYTHON!"=="" (
        where %%P >nul 2>&1
        if not errorlevel 1 (
            %%P -c "import sys; sys.exit(0 if sys.version_info >= (3,11) else 1)" >nul 2>&1
            if not errorlevel 1 (
                set PYTHON=%%P
            )
        )
    )
)

if "!PYTHON!"=="" (
    echo.
    echo   ERROR: Python 3.11+ is required but was not found.
    echo.
    echo   Install it from:  https://www.python.org/downloads/
    echo   Make sure to tick "Add Python to PATH" during installation.
    echo.
    pause
    exit /b 1
)

:: ── Run installer ───────────────────────────────────────────────────────────
!PYTHON! install.py

echo.
pause
