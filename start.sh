#!/bin/bash
CWD=$(cd "$(dirname $0)";pwd)
"$CWD"/easydarwin install
"$CWD"/easydarwin start
PID=$(ps -ef | grep "easydarwin" | grep -v grep | awk '{print $2}')
prlimit --pid ${PID} --nofile=40000:40000