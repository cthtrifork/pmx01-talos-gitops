#!/bin/sh
# Init container: sign a short-lived GitHub App JWT (RS256) with the App's private key.
set -eu

b64url() { openssl base64 -A | tr '+/' '-_' | tr -d '='; }

now=$(date +%s)
app_id=$(tr -d '\n' </app/app-id)
header=$(printf '{"alg":"RS256","typ":"JWT"}' | b64url)
payload=$(printf '{"iat":%d,"exp":%d,"iss":"%s"}' $((now - 60)) $((now + 540)) "$app_id" | b64url)
signature=$(printf '%s.%s' "$header" "$payload" | openssl dgst -sha256 -sign /app/private-key.pem | b64url)

umask 077
printf '%s.%s.%s' "$header" "$payload" "$signature" >/work/jwt
