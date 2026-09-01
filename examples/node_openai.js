import OpenAI from "openai";

// Simply point baseURL to Kurisu!
const openai = new OpenAI({
  baseURL: "http://localhost:8080/v1",
  apiKey: "sk-kurisu-local-master-key",
});

async function main() {
  console.log("⚡ Querying Kurisu Universal Gateway...");

  const completion = await openai.chat.completions.create({
    model: "fast", // Resolves to gpt-4o-mini
    messages: [{ role: "user", content: "Hello from Node.js!" }],
  });

  console.log("Response:", completion.choices[0].message.content);
}

main().catch(console.error);
