python3 ollama_bench.py \
  --base-url http://localhost:11434 \
  --models llama3.2:1b llama3.2:3b qwen2.5:1.5b qwen3:1.7b \
  --warm-runs 1 \
  --concurrency 1 2 \
  --auto-pull \
  --skip-unresolved \
  --output ollama_bench_light.json