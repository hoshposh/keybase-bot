#!/bin/bash
# test_feedly.sh
# Simulates a Feedly NewEntrySaved webhook POST request with HMAC-SHA256 signature

PORT=${1:-8080}
SECRET=${2:-"my-secret-token"}
URL="http://localhost:$PORT/webhooks/feedly"

echo "Sending mock Feedly webhook to $URL..."

BODY='{
    "title": "Understanding the Model Context Protocol",
    "entryUrl": "https://example.com/mcp-intro",
    "content": "MCP is an open standard that enables AI models to securely access local data.",
    "author": "Jane Doe",
    "publishedDate": 1711200000000
}'

# Calculate HMAC-SHA256 of the body using the secret
# The explicit -n is omitted because the variable should be passed carefully, or better use printf
SIGNATURE=$(printf "%s" "$BODY" | openssl dgst -sha256 -mac HMAC -macopt hexkey:$(echo -n "$SECRET" | xxd -p) | awk '{print $NF}')
# Alternatively, a more standard syntax:
SIGNATURE=$(printf "%s" "$BODY" | openssl dgst -sha256 -hmac "$SECRET" | sed 's/^.* //')

curl -v -X POST "$URL" \
  -H "Content-Type: application/json" \
  -H "X-Feedly-Signature: $SIGNATURE" \
  -d "$BODY"

echo ""
echo "Done."
