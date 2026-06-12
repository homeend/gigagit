@echo off
rem Run gigagit's test suite in stages: quality gates, unit tests, then the
rem e2e scenario suite LAST (it exercises the full CLI-engine-git stack and
rem only makes sense once everything else is green).
rem
rem   test.cmd            gates + unit + e2e
rem   test.cmd unit       unit tests only (./cmd/... ./internal/...)
rem   test.cmd e2e        e2e scenarios only (./e2e)
rem   test.cmd race       gates + unit + e2e, all with -race (pre-merge gate)
rem
rem Append -v to any form for verbose output - e2e scenarios then report what
rem each one verified and every gg command's exit, e.g.  test.cmd e2e -v
setlocal EnableDelayedExpansion

rem Run from the project root (this script's directory) regardless of CWD.
cd /d "%~dp0"

set "RACE="
set "VERBOSE="
set "TARGET=%~1"
if "%TARGET%"=="" set "TARGET=all"
if /i "%TARGET%"=="-v" (
	set "TARGET=all"
	set "VERBOSE=-v"
)
if /i "%~2"=="-v" set "VERBOSE=-v"

if /i "%TARGET%"=="unit" (
	call :unit
) else if /i "%TARGET%"=="e2e" (
	call :e2e
) else if /i "%TARGET%"=="race" (
	set "RACE=-race"
	call :gates
	if errorlevel 1 goto :fail
	call :unit
	if errorlevel 1 goto :fail
	call :e2e
) else if /i "%TARGET%"=="all" (
	call :gates
	if errorlevel 1 goto :fail
	call :unit
	if errorlevel 1 goto :fail
	call :e2e
) else (
	echo usage: %~nx0 [unit^|e2e^|race] [-v] 1>&2
	exit /b 2
)
if errorlevel 1 goto :fail

echo all green
endlocal
exit /b 0

:gates
echo == quality gates: go vet + gofmt ==
go vet ./...
if errorlevel 1 exit /b 1
set "UNFORMATTED="
for /f "delims=" %%i in ('gofmt -l internal/ cmd/ e2e/') do set "UNFORMATTED=%%i"
if defined UNFORMATTED (
	echo gofmt: files need formatting ^(run gofmt -w^): !UNFORMATTED! 1>&2
	exit /b 1
)
exit /b 0

:unit
echo == unit tests ==
go test %RACE% %VERBOSE% ./cmd/... ./internal/...
exit /b %errorlevel%

:e2e
echo == e2e scenarios (last: full CLI-engine-git stack) ==
go test %RACE% %VERBOSE% ./e2e/
exit /b %errorlevel%

:fail
echo tests failed 1>&2
endlocal
exit /b 1
