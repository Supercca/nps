@echo off
setlocal enabledelayedexpansion
cd /d "%~dp0"

rem ============================================================
rem  Build NPS server release package for windows/amd64
rem  Usage:
rem     build_windows_amd64_server.bat            use default version
rem     build_windows_amd64_server.bat 0.26.102    override version
rem  Output: windows_amd64_server.tar.gz
rem ============================================================

set "VERSION=0.26.102"
set "PKG=windows_amd64_server"
rem PACK_CONF=0  ship the nps.exe binary only, conf is generated on first run
rem PACK_CONF=1  also ship conf/nps.conf and conf/global.json templates
set "PACK_CONF=0"

if not "%~1"=="" set "VERSION=%~1"

set "STAGE=%CD%\build\%PKG%"
set "OUT=%CD%\%PKG%.tar.gz"
set "TMPTAR=%TEMP%\%PKG%.tar"
set "GNUTAR="
set "GZIPEXE="

echo.
echo ==========================================================
echo   NPS  windows/amd64  server   v%VERSION%
echo ==========================================================
echo.

rem ---------- 1/7 check go ----------
where go >nul 2>nul
if errorlevel 1 (
    echo [x] go not found in PATH, please install Go first
    exit /b 1
)
for /f "delims=" %%v in ('go version') do echo [1/7] %%v

rem ---------- 2/7 check version ----------
if not exist "lib\version\version.go" (
    echo [x] lib\version\version.go not found, run this script at repo root
    exit /b 1
)
findstr /c:"const VERSION" "lib\version\version.go" | findstr /c:"%VERSION%" >nul
if errorlevel 1 (
    echo [x] version mismatch in source:
    findstr /c:"const VERSION" "lib\version\version.go"
    echo     please set it to:  const VERSION = "%VERSION%"
    exit /b 1
)
echo [2/7] version ok: v%VERSION%

rem ---------- 3/7 clean stage ----------
if exist "%STAGE%" rmdir /s /q "%STAGE%"
mkdir "%STAGE%" 2>nul
if exist "%OUT%" del /q "%OUT%"
if exist "%TMPTAR%" del /q "%TMPTAR%"
echo [3/7] stage dir build\%PKG%

rem ---------- 4/7 cross compile ----------
set "CGO_ENABLED=0"
set "GOOS=windows"
set "GOARCH=amd64"
set "GOARM="
echo [4/7] building: GOOS=windows GOARCH=amd64 CGO_ENABLED=0
go build -trimpath -buildvcs=false -ldflags "-s -w" -o "%STAGE%\nps.exe" .\cmd\nps\nps.go
if errorlevel 1 (
    echo [x] build failed
    exit /b 1
)
if not exist "%STAGE%\nps.exe" (
    echo [x] nps.exe was not produced
    exit /b 1
)
for %%A in ("%STAGE%\nps.exe") do set /a "SZ=%%~zA/1048576"
echo       nps.exe built, about !SZ! MB

rem ---------- 5/7 prepare conf ----------
if "%PACK_CONF%"=="1" (
    mkdir "%STAGE%\conf" 2>nul
    copy /y "conf\nps.conf" "%STAGE%\conf\" >nul
    copy /y "conf\global.json" "%STAGE%\conf\" >nul
    echo [5/7] conf templates included
    echo       WARNING: nps.conf contains the repo default passwords, change them
) else (
    echo [5/7] conf not included, first run of .\nps.exe generates it with random secrets
)

rem ---------- 6/7 find tar ----------
rem GNU tar from Git for Windows is preferred, otherwise fall back to the
rem system tar (Windows 10+ ships bsdtar, which handles -z internally).
for /f "delims=" %%G in ('where git 2^>nul') do call :probe_git "%%~dpG"
for /f "delims=" %%T in ('where tar 2^>nul') do call :probe_tar "%%T"

if defined GNUTAR (
    echo [6/7] packaging with GNU tar
    pushd "%STAGE%"
    if exist "conf\nps.conf" "%GNUTAR%" --force-local -cf "%TMPTAR%" conf/nps.conf conf/global.json
    "%GNUTAR%" --force-local -rf "%TMPTAR%" nps.exe
    if errorlevel 1 (
        popd
        echo [x] tar failed
        exit /b 1
    )
    popd
    if defined GZIPEXE (
        "%GZIPEXE%" -9 -n -c "%TMPTAR%" > "%OUT%"
    ) else (
        "%GNUTAR%" --force-local -czf "%OUT%" -C "%STAGE%" nps.exe conf
    )
    if errorlevel 1 (
        if exist "%TMPTAR%" del /q "%TMPTAR%"
        echo [x] gzip failed
        exit /b 1
    )
    if exist "%TMPTAR%" del /q "%TMPTAR%"
) else (
    echo [6/7] GNU tar not found, falling back to system tar
    pushd "%STAGE%"
    if exist conf (
        tar -czf "%OUT%" nps.exe conf
    ) else (
        tar -czf "%OUT%" nps.exe
    )
    if errorlevel 1 (
        popd
        echo [x] tar failed
        exit /b 1
    )
    popd
)

if not exist "%OUT%" (
    echo [x] %PKG%.tar.gz was not produced
    exit /b 1
)
for %%A in ("%OUT%") do if %%~zA LSS 1024 (
    echo [x] %PKG%.tar.gz looks empty
    exit /b 1
)

rem ---------- 7/7 verify ----------
echo [7/7] archive content:
if defined GNUTAR (
    "%GNUTAR%" --force-local -tvf "%OUT%"
    if errorlevel 1 (
        echo       GNU tar cannot read the gzip archive, retry with system tar
        tar -tvf "%OUT%"
    )
) else (
    tar -tvf "%OUT%"
)
for %%A in ("%OUT%") do set /a "SZ=%%~zA/1024"
for /f "delims=" %%H in ('powershell -NoProfile -Command "(Get-FileHash -LiteralPath '%OUT%' -Algorithm SHA256).Hash"') do set "SHA=%%H"

echo.
echo ==========================================================
echo   done: %OUT%
echo   version: v%VERSION%    size: !SZ! KB
echo   sha256 : !SHA!
echo ==========================================================
echo.
echo next steps:
echo   1. upload and extract:  tar -zxvf %PKG%.tar.gz
echo   2. first run:           .\nps.exe -server   (install / start menu)
echo   3. or run directly:     .\nps.exe           (conf auto-generated, see logs)
echo.
exit /b 0

rem ============================================================
rem  helpers
rem ============================================================
:probe_git
if defined GNUTAR goto :eof
for %%D in ("%~1..") do set "GROOT=%%~fD"
set "CAND=%GROOT%\usr\bin\tar.exe"
if not exist "%CAND%" goto :eof
"%CAND%" --version 2>nul | findstr /c:"GNU tar" >nul
if errorlevel 1 goto :eof
set "GNUTAR=%CAND%"
set "GZIPEXE=%GROOT%\usr\bin\gzip.exe"
rem GNU tar spawns gzip by name, so its bin dir must be on PATH
set "PATH=%GROOT%\usr\bin;%PATH%"
goto :eof

:probe_tar
if defined GNUTAR goto :eof
if not exist "%~1" goto :eof
"%~1" --version 2>nul | findstr /c:"GNU tar" >nul
if errorlevel 1 goto :eof
set "GNUTAR=%~1"
for %%D in ("%~dp1..") do if exist "%%~fD\gzip.exe" set "GZIPEXE=%%~fD\gzip.exe"
if defined GZIPEXE for %%D in ("%~dp1..") do set "PATH=%%~fD;!PATH!"
goto :eof
