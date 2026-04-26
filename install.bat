@echo off
setlocal enabledelayedexpansion

cd /d "%~dp0"
set CERT_PATH=%USERPROFILE%\.agentproxy\certs\agentproxy-ca-cert.pem

where go >nul 2>&1
if errorlevel 1 (
    echo.
    echo   ERROR: Go is required but was not found.
    echo   Install Go from https://go.dev/dl/ and re-run this script.
    echo.
    pause
    exit /b 1
)

for /f "tokens=3" %%V in ('go version') do echo Using Go: %%V

echo Building agentproxy.exe ...
go build -o agentproxy.exe ./cmd/agentproxy
if errorlevel 1 (
    echo Build failed.
    pause
    exit /b 1
)
echo Built agentproxy.exe

echo.
echo Setting up CA cert and directories ...
agentproxy.exe ca-setup
if errorlevel 1 (
    echo CA setup failed.
    pause
    exit /b 1
)

echo.
certutil -verify "%CERT_PATH%" >nul 2>&1
if not errorlevel 1 (
    echo CA cert already trusted
) else (
    echo Trusting CA cert in the Windows Root store ...
    certutil -addstore "Root" "%CERT_PATH%"
    if errorlevel 1 (
        echo Automatic trust failed. Run this in an elevated shell:
        echo   certutil -addstore "Root" "%CERT_PATH%"
    ) else (
        echo CA cert trusted
    )
)

set INSTALL_DIR=%USERPROFILE%\.local\bin
if not exist "%INSTALL_DIR%" mkdir "%INSTALL_DIR%"

echo.
echo Installing to %INSTALL_DIR% ...

copy /y agentproxy.exe "%INSTALL_DIR%\agentproxy.exe" >nul
echo Installed agentproxy.exe

(
echo @echo off
echo setlocal
echo set HOST=%%AGENTPROXY_HOST%%
echo if "%%HOST%%"=="" set HOST=127.0.0.1
echo set PORT=%%AGENTPROXY_PORT%%
echo if "%%PORT%%"=="" set PORT=7717
echo set CERT_PATH=%%AGENTPROXY_CA_CERT%%
echo if "%%CERT_PATH%%"=="" set CERT_PATH=%%USERPROFILE%%\.agentproxy\certs\agentproxy-ca-cert.pem
echo agentproxy.exe status --host "%%HOST%%" --port "%%PORT%%" -q ^>nul 2^>^&1
echo if errorlevel 1 ^{
echo   echo AgentProxy is not running on %%HOST%%:%%PORT%%
echo   echo Start it with: agentproxy-start
echo   exit /b 1
echo ^}
echo set HTTP_PROXY=http://%%HOST%%:%%PORT%%
echo set HTTPS_PROXY=http://%%HOST%%:%%PORT%%
echo set NODE_EXTRA_CA_CERTS=%%CERT_PATH%%
echo set SSL_CERT_FILE=%%CERT_PATH%%
echo set REQUESTS_CA_BUNDLE=%%CERT_PATH%%
echo set CURL_CA_BUNDLE=%%CERT_PATH%%
echo claude %%*
) > "%INSTALL_DIR%\claudeproxy.bat"

(
echo @echo off
echo setlocal
echo set HOST=%%AGENTPROXY_HOST%%
echo if "%%HOST%%"=="" set HOST=127.0.0.1
echo set PORT=%%AGENTPROXY_PORT%%
echo if "%%PORT%%"=="" set PORT=7717
echo set CERT_PATH=%%AGENTPROXY_CA_CERT%%
echo if "%%CERT_PATH%%"=="" set CERT_PATH=%%USERPROFILE%%\.agentproxy\certs\agentproxy-ca-cert.pem
echo agentproxy.exe status --host "%%HOST%%" --port "%%PORT%%" -q ^>nul 2^>^&1
echo if errorlevel 1 ^{
echo   echo AgentProxy is not running on %%HOST%%:%%PORT%%
echo   echo Start it with: agentproxy-start
echo   exit /b 1
echo ^}
echo set HTTP_PROXY=http://%%HOST%%:%%PORT%%
echo set HTTPS_PROXY=http://%%HOST%%:%%PORT%%
echo set NODE_EXTRA_CA_CERTS=%%CERT_PATH%%
echo set SSL_CERT_FILE=%%CERT_PATH%%
echo set REQUESTS_CA_BUNDLE=%%CERT_PATH%%
echo set CURL_CA_BUNDLE=%%CERT_PATH%%
echo codex %%*
) > "%INSTALL_DIR%\codexproxy.bat"

(
echo @echo off
echo setlocal
echo set HOST=%%AGENTPROXY_HOST%%
echo if "%%HOST%%"=="" set HOST=127.0.0.1
echo set PORT=%%AGENTPROXY_PORT%%
echo if "%%PORT%%"=="" set PORT=7717
echo set CERT_PATH=%%AGENTPROXY_CA_CERT%%
echo if "%%CERT_PATH%%"=="" set CERT_PATH=%%USERPROFILE%%\.agentproxy\certs\agentproxy-ca-cert.pem
echo agentproxy.exe status --host "%%HOST%%" --port "%%PORT%%" -q ^>nul 2^>^&1
echo if errorlevel 1 ^{
echo   echo AgentProxy is not running on %%HOST%%:%%PORT%%
echo   echo Start it with: agentproxy-start
echo   exit /b 1
echo ^}
echo set HTTP_PROXY=http://%%HOST%%:%%PORT%%
echo set HTTPS_PROXY=http://%%HOST%%:%%PORT%%
echo set NODE_EXTRA_CA_CERTS=%%CERT_PATH%%
echo set SSL_CERT_FILE=%%CERT_PATH%%
echo set REQUESTS_CA_BUNDLE=%%CERT_PATH%%
echo set CURL_CA_BUNDLE=%%CERT_PATH%%
echo gh copilot %%*
) > "%INSTALL_DIR%\copilotproxy.bat"

(
echo @echo off
echo setlocal
echo set HOST=%%AGENTPROXY_HOST%%
echo set PORT=%%AGENTPROXY_PORT%%
echo if "%%HOST%%"=="" ^{
echo   if "%%PORT%%"=="" ^{
echo     agentproxy.exe start %%*
echo   ^) else ^{
echo     agentproxy.exe start --port "%%PORT%%" %%*
echo   ^}
echo ^} else ^{
echo   if "%%PORT%%"=="" ^{
echo     agentproxy.exe start --host "%%HOST%%" %%*
echo   ^} else ^{
echo     agentproxy.exe start --host "%%HOST%%" --port "%%PORT%%" %%*
echo   ^}
echo ^}
) > "%INSTALL_DIR%\agentproxy-start.bat"

echo Installed: claudeproxy, codexproxy, copilotproxy, agentproxy-start

echo.
echo  NOTE: Add %INSTALL_DIR% to your PATH if not already present.
echo  In PowerShell (user-level, permanent^):
echo    [Environment]::SetEnvironmentVariable("Path", $env:Path + ";%INSTALL_DIR%", "User")

echo.
echo --------------------------------------------------------
echo  AgentProxy installed successfully.
echo.
echo  Start the proxy:   agentproxy-start
echo  Claude via proxy:  claudeproxy ...
echo  Codex via proxy:   codexproxy ...
echo  Copilot via proxy: copilotproxy ...
echo --------------------------------------------------------
echo.
pause
