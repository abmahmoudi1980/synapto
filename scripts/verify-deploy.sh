#!/bin/sh
set -e
CJAR=/tmp/cookies.txt
rm -f "$CJAR"
curl -sS -c "$CJAR" -X POST -H 'Content-Type: application/json' \
  -d '{"password":"Mehrnaz@1387"}' http://127.0.0.1:8080/api/auth/login > /dev/null
echo "--- with cookie, verbose ---"
curl -sS -b "$CJAR" -v 'http://127.0.0.1:8080/api/posts?status=sent&limit=3' 2>&1
