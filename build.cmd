@echo off
rem Build the gg executable for Linux and Windows into the project root.
rem
rem   build.cmd            build both: gg (linux) and gg.exe (windows)
rem   build.cmd linux      build only gg
rem   build.cmd windows    build only gg.exe
rem
rem GOARCH may be overridden (default: amd64), e.g.  set GOARCH=arm64 && build.cmd windows
setlocal EnableDelayedExpansion

rem Run from the project root (this script's directory) regardless of CWD.
cd /d "%~dp0"

set "PKG=.\cmd\gg"
if not defined GOARCH set "GOARCH=amd64"

rem Version metadata injected into internal/buildinfo via -ldflags.
set "VERSION="
for /f "delims=" %%i in ('git describe --tags --always --dirty 2^>nul') do set "VERSION=%%i"
if not defined VERSION set "VERSION=dev"
set "COMMIT="
for /f "delims=" %%i in ('git rev-parse --short HEAD 2^>nul') do set "COMMIT=%%i"
if not defined COMMIT set "COMMIT=none"

set "LDFLAGS=-s -w -X github.com/gigagit/gg/internal/buildinfo.Version=!VERSION! -X github.com/gigagit/gg/internal/buildinfo.Commit=!COMMIT!"

set "TARGET=%~1"
if "%TARGET%"=="" set "TARGET=all"

if /i "%TARGET%"=="linux" (
	call :build linux gg
) else if /i "%TARGET%"=="windows" (
	call :build windows gg.exe
) else if /i "%TARGET%"=="all" (
	call :build linux gg
	if errorlevel 1 goto :fail
	call :build windows gg.exe
) else (
	echo usage: %~nx0 [linux^|windows^|all] 1>&2
	exit /b 2
)
if errorlevel 1 goto :fail

echo done: !VERSION! ^(!COMMIT!^)
endlocal
exit /b 0

:build
set "GOOS=%~1"
set "OUT=%~2"
echo building !OUT! ^(!GOOS!/%GOARCH%^) ...
set "CGO_ENABLED=0"
go build -trimpath -ldflags "%LDFLAGS%" -o "!OUT!" "%PKG%"
exit /b %errorlevel%

:fail
echo build failed 1>&2
endlocal
exit /b 1
