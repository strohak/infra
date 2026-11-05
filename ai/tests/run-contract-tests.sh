#!/usr/bin/env bash
# run-contract-tests.sh: prove the inference contract check catches what it should.
#
# Needs: ansible-playbook on PATH, python3, and Qdrant on 127.0.0.1:6333/6334
# started with QDRANT__SERVICE__API_KEY=$QDRANT_API_KEY, e.g.
#   docker run -d -p 127.0.0.1:6333:6333 -p 127.0.0.1:6334:6334 \
#     -e QDRANT__SERVICE__API_KEY=test-key-123 docker.io/qdrant/qdrant:v1.20.0
#
# Each case starts the stub backend with a behaviour, runs the real roles, and
# checks the run passes or fails with the expected message.

set -euo pipefail

cd "$(dirname "$0")"
export ANSIBLE_CONFIG="$PWD/ansible.cfg"
QDRANT_API_KEY="${QDRANT_API_KEY:-test-key-123}"
ALIASES="ai:chat,ai:fast,ai:embed"
port=18100
pass=0
fail=0
stub_pid=""

cleanup() {
  if [[ -n "$stub_pid" ]]; then
    kill "$stub_pid" 2>/dev/null || true
  fi
}
trap cleanup EXIT

start_stub() { # env assignments for the stub...
  cleanup
  port=$((port + 1))
  env "$@" STUB_PORT="$port" python3 -I stub_server.py &
  stub_pid=$!
  for _ in $(seq 1 50); do
    curl -fs "http://127.0.0.1:$port/api/version" >/dev/null 2>&1 && return 0
    sleep 0.1
  done
  echo "stub did not start" >&2
  exit 1
}

# case <name> <expect: pass|fail> <message substring or ""> <extra ansible args...>
run_case() {
  local name=$1 expect=$2 want=$3
  shift 3
  local out rc=0
  out=$(ansible-playbook -i localhost, contract.yml -e stub_port="$port" -e qdrant_api_key="$QDRANT_API_KEY" \
    -e inference_verify_tool_retries=1 "$@" 2>&1) || rc=$?
  local ok=1
  if [[ $expect == pass && $rc -ne 0 ]]; then ok=0; fi
  if [[ $expect == fail && $rc -eq 0 ]]; then ok=0; fi
  if [[ -n $want ]] && ! grep -qF -- "$want" <<<"$out"; then ok=0; fi
  if [[ $ok -eq 1 ]]; then
    echo "PASS  $name"
    pass=$((pass + 1))
  else
    echo "FAIL  $name (exit $rc, wanted $expect${want:+ with \"$want\"})"
    echo "$out" | tail -25 | sed 's/^/      /'
    fail=$((fail + 1))
  fi
}

start_stub STUB_MODELS="$ALIASES"
run_case "contract passes" pass "store + search OK"
run_case "second run changes nothing" pass "changed=0"

start_stub STUB_MODELS="$ALIASES" STUB_DIMS=384
run_case "wrong embedding size is caught" fail "returned 384 dimensions, but the model set declares 768"

start_stub STUB_MODELS="$ALIASES" STUB_NO_TOOLS=1
run_case "missing tool calls are caught" fail "did not return a get_weather tool call"

start_stub STUB_MODELS="ai:chat,ai:fast"
run_case "missing model alias is caught" fail "Model 'ai:embed' is not listed by /v1/models"

start_stub STUB_MODELS="$ALIASES"
run_case "unknown backend is rejected" fail "Unknown inference_backend 'vllm'" -e inference_backend=vllm
run_case "model override without this backend is rejected" fail "has no 'ollama' model" \
  -e '{"ai_models":{"chat":{"llamacpp":"x"},"fast":{"ollama":"f"},"embed":{"ollama":"e","dims":8}}}'
run_case "12 GB NVIDIA picks the 8gb tier" pass "tier=8gb chat=qwen3:8b fast=qwen3:4b" \
  -e ai_accelerator=nvidia -e ai_vram_mb=12282
run_case "24 GB NVIDIA picks the 24gb tier" pass "tier=24gb" -e ai_accelerator=nvidia -e ai_vram_mb=24576
run_case "CPU-only picks the cpu tier" pass "tier=cpu" -e ai_accelerator=cpu -e ai_vram_mb=0

echo
echo "$pass passed, $fail failed"
[[ $fail -eq 0 ]]
