# 1. Standard OpenAI-compatible Chat Completion
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer sk-kurisu-local-master-key" \
  -d '{
    "model": "smart",
    "messages": [
      {"role": "system", "content": "You are a helpful assistant."},
      {"role": "user", "content": "Explain quantum computing in one short sentence."}
    ],
    "temperature": 0.7
  }'

# 2. Real-time Streaming (SSE)
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer sk-kurisu-local-master-key" \
  -d '{
    "model": "claude-3-5-sonnet-20241022",
    "stream": true,
    "messages": [
      {"role": "user", "content": "Count from 1 to 5 slowly."}
    ]
  }'

# 3. List available models & aliases
curl -X GET http://localhost:8080/v1/models \
  -H "Authorization: Bearer sk-kurisu-local-master-key"

# 4. Gateway Health & Real-time Stats
curl -X GET http://localhost:8080/health
curl -X GET http://localhost:8080/stats
