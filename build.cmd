@echo off
rem Build the gg executable for Linux and Windows into the project root.
rem
rem   build.cmd            build both: gg (linux) and gg.exe (windows)
rem   build.cmd linux      build only gg
rem   build.cmd windows    build only gg.exe
rem   build.cmd web        build gg-web-new.exe beside this script (see below)
rem
rem The `web` target exists because run-win.cmd cannot overwrite a running
rem gg-web.exe -- Windows locks it. It writes gg-web-new.exe instead, which
rem run-win.cmd renames into place on the next launch. Run this in the
rem worktree you want to serve; the exe lands next to this script, so a build
rem from the wrong worktree is visible in the path it prints.
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

set "LDFLAGS=-s -w -X github.com/homeend/gigagit/internal/buildinfo.Version=!VERSION! -X github.com/homeend/gigagit/internal/buildinfo.Commit=!COMMIT!"

set "TARGET=%~1"
if "%TARGET%"=="" set "TARGET=windows"

if /i "%TARGET%"=="linux" (
	call :build linux gg
) else if /i "%TARGET%"=="windows" (
	call :build windows gg.exe
) else if /i "%TARGET%"=="web" (
	call :buildweb
) else if /i "%TARGET%"=="all" (
	call :build linux gg
	if errorlevel 1 goto :fail
	call :build windows gg.exe
) else (
	echo usage: %~nx0 [linux^|windows^|all^|web] 1>&2
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

:buildweb
rem -buildvcs=false: a worktree created from WSL has a .git FILE pointing at a
rem Linux path Windows cannot read, and Go's VCS stamping treats an
rem unreadable-but-present repo as a hard error. The version still reaches the
rem binary through -ldflags above (falling back to "dev" when git is likewise
rem unreadable), so nothing is lost by skipping the stamp.
echo building gg-web-new.exe ^(windows/%GOARCH%^) in "%CD%" ...
set "GOOS=windows"
set "CGO_ENABLED=0"
go build -trimpath -buildvcs=false -ldflags "%LDFLAGS%" -o "gg-web-new.exe" "%PKG%"
if errorlevel 1 exit /b 1
echo wrote "%CD%\gg-web-new.exe" - run-win.cmd swaps it in on next launch
exit /b 0

:fail
echo build failed 1>&2
endlocal
exit /b 1
