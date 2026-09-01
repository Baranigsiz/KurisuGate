"""
Kurisu (クリス) Drop-in Integration with standard OpenAI Python SDK
"""
import os
from openai import OpenAI

# Simply point base_url to Kurisu Gateway!
client = OpenAI(
    base_url="http://localhost:8080/v1",
    api_key="sk-kurisu-local-master-key",  # or any key if unauthenticated
)

def test_chat():
    print("--- 1. Testing Model Alias ('smart' -> gpt-4o with automatic fallback) ---")
    response = client.chat.completions.create(
        model="smart",
        messages=[
            {"role": "system", "content": "You are a concise AI assistant."},
            {"role": "user", "content": "What is the airspeed velocity of an unladen swallow?"},
        ],
    )
    print(f"Response: {response.choices[0].message.content}\n")

def test_streaming():
    print("--- 2. Testing Streaming Claude 3.5 Sonnet through Kurisu Gateway ---")
    stream = client.chat.completions.create(
        model="claude-3-5-sonnet-20241022",
        messages=[
            {"role": "user", "content": "Write a haiku about high-performance Go proxies."}
        ],
        stream=True,
    )
    for chunk in stream:
        if chunk.choices[0].delta.content is not None:
            print(chunk.choices[0].delta.content, end="", flush=True)
    print("\n")

if __name__ == "__main__":
    test_chat()
    test_streaming()
