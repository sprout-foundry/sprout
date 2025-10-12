#!/bin/bash
# ZAI API Request - 20251012_163958
# Run this script to reproduce the exact request

API_KEY="${ZAI_API_KEY:-your_api_key_here}"

curl -s -X POST "https://api.z.ai/api/coding/paas/v4/chat/completions" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $API_KEY" \
  -d '{"frequency_penalty":0.5,"messages":[{"content":"Say hello in exactly one word.","role":"user"}],"model":"GLM-4.6","presence_penalty":0.3,"stream":true,"temperature":0.1}' | jq '.'
