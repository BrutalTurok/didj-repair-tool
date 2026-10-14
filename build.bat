@echo off
rem Builds the Windows executables. Requires Go: https://go.dev/dl/
if not exist dist mkdir dist
set GOOS=windows
set GOARCH=amd64
go build -trimpath -ldflags "-s -w" -o dist\DidjRepairTool.exe . || exit /b 1
set GOARCH=386
go build -trimpath -ldflags "-s -w" -o dist\DidjRepairTool_32bit.exe . || exit /b 1
echo Done: dist\DidjRepairTool.exe and dist\DidjRepairTool_32bit.exe
