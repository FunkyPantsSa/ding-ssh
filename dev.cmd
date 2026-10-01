@ECHO OFF
REM ding-ssh dev launcher
REM
REM This machine's policy forbids running .ps1 files, so this wrapper starts
REM PowerShell with -ExecutionPolicy Bypass and activates the environment.
REM All tool locations are resolved inside dev-env.ps1.
REM
REM Usage:
REM   dev.cmd                          open an activated shell in this folder
REM   dev.cmd wails dev                run one command with the environment ready
REM   dev.cmd go test ./internal/...

SETLOCAL
SET "ROOT=%~dp0"

IF "%~1"=="" (
  powershell.exe -NoProfile -ExecutionPolicy Bypass -NoExit -Command ". '%ROOT%dev-env.ps1'"
) ELSE (
  powershell.exe -NoProfile -ExecutionPolicy Bypass -Command ". '%ROOT%dev-env.ps1'; & %*"
  SET "RC=%ERRORLEVEL%"
  ENDLOCAL & EXIT /B %RC%
)
ENDLOCAL
