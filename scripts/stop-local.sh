#!/bin/bash
# Stop the local Synapto admin service.
PIDFILE="/home/abolfazl/apps/synapto/.runtime/assistant.pid"
if [ -f "$PIDFILE" ]; then
  PID=$(cat "$PIDFILE")
  if kill -0 "$PID" 2>/dev/null; then
    kill "$PID" && echo "Stopped PID $PID"
  else
    echo "PID $PID not running"
  fi
  rm -f "$PIDFILE"
else
  pkill -f "bin/assistant" 2>/dev/null && echo "Stopped via pkill" || echo "No running instance found"
fi
