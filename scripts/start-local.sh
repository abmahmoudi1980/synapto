#!/bin/bash
# Start the Synapto admin service in the background, fully detached.
set -e
cd /home/abolfazl/apps/synapto

# Stop any existing instance.
pkill -f "bin/assistant" 2>/dev/null || true
sleep 0.3

# Source env, redirect everything, double-fork via setsid.
set -a
source .runtime/local.env
set +a

setsid nohup ./bin/assistant > .runtime/assistant.log 2>&1 < /dev/null &
PID=$!
disown
echo $PID > .runtime/assistant.pid
echo "Started PID $PID"
