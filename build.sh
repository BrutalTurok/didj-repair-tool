#!/bin/sh
# Builds the Windows executables (works from Linux, macOS or Windows with Go installed).
set -e
mkdir -p dist
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o dist/DidjRepairTool.exe .
GOOS=windows GOARCH=386   go build -trimpath -ldflags "-s -w" -o dist/DidjRepairTool_32bit.exe .
echo "Done: dist/DidjRepairTool.exe and dist/DidjRepairTool_32bit.exe"
