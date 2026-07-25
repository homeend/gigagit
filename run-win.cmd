@echo off
rem gg web serves the CURRENT directory; the exe can live anywhere.
rem This worktree was created under WSL, so Windows git cannot read its
rem .git link — serve the main checkout instead (same repo, same objects).
rem
rem Rebuilds from WSL land as gg-web-new.exe (Windows locks a running exe,
rem so WSL can't overwrite gg-web.exe in place); swap it in on startup.
set WD=t:\others\gigagit.worktrees\web-dev
if exist %WD%\gg-web-new.exe (
  del %WD%\gg-web.exe 2>nul
  if not exist %WD%\gg-web.exe (
    ren %WD%\gg-web-new.exe gg-web.exe
  ) else (
    echo gg-web.exe is still running - close it and re-run this script.
    exit /b 1
  )
)
cd /d t:\others\gigagit
%WD%\gg-web.exe web --open
