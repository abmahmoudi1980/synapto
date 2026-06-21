#!/bin/bash
# Start the Synapto admin service in the background, fully detached.
# Uses Track A env (fake Telegram + fake AI) for local dev.
set -e
cd /home/abolfazl/apps/synapto

# Stop any existing instance.
pkill -f "bin/assistant" 2>/dev/null || true
sleep 0.3

# Write Track A env if missing (no creds required).
make env-track-a >/dev/null

# Source env, redirect everything, double-fork via setsid.
set -a
source .runtime/track-a.env
set +a

setsid nohup ./bin/assistant > .runtime/assistant.log 2>&1 < /dev/null &
PID=$!
disown
echo $PID > .runtime/assistant.pid
echo "Started PID $PID"
