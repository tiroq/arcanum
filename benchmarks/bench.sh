python3 ollama_bench.py \
  --base-url http://localhost:11434 \
  --models qwen2.5:7b-instruct qwen3:8b qwen3:14b llama3.2:3b qwen3.5:9b \
  --warm-runs 3 \
  --concurrency 1 2 4 \
  --auto-pull \
  --force-unload \
  --output bench_v2.json