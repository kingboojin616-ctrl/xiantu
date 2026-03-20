#!/usr/bin/env bash
# xiantu API automated test runner (shell version)
# Usage: ./test/run.sh [BASE_URL]
set -uo pipefail

BASE_URL="${1:-${BASE_URL:-https://xiantu-server-production.up.railway.app}}"
BASE_URL="${BASE_URL%/}"

GREEN='\033[32m'; RED='\033[31m'; YELLOW='\033[33m'; RESET='\033[0m'
PASSED=0; FAILED=0

pass() { printf "[${GREEN}✓ PASS${RESET}] %s — %s\n" "$1" "$2"; ((PASSED++)) || true; }
fail() { printf "[${RED}✗ FAIL${RESET}] %s — %s\n" "$1" "$2"; ((FAILED++)) || true; }

# curl wrapper: $1=method $2=path $3=body $4=token
api() {
  local method="$1" path="$2" body="${3:-}" token="${4:-}"
  local args=(-s -w '\n%{http_code}' -m 15)
  [[ -n "$token" ]] && args+=(-H "Authorization: Bearer $token")
  if [[ "$method" == "POST" ]]; then
    args+=(-X POST -H "Content-Type: application/json")
    [[ -n "$body" ]] && args+=(-d "$body")
  fi
  curl "${args[@]}" "${BASE_URL}${path}" 2>/dev/null || echo -e "\n000"
}

parse_status() { echo "$1" | tail -1; }
parse_body()   { echo "$1" | sed '$d'; }

# json array length — works for top-level arrays and {"data":[...]} wrappers
json_array_len() {
  local body="$1" key="${2:-}"
  local len
  if [[ -n "$key" ]]; then
    len=$(echo "$body" | python3 -c "
import sys,json
d=json.load(sys.stdin)
arr=d.get('$key') or d.get('data',{}).get('$key') if isinstance(d.get('data'),dict) else None
if arr is None:
    for v in d.values():
        if isinstance(v,list): arr=v; break
print(len(arr) if isinstance(arr,list) else -1)
" 2>/dev/null)
  else
    len=$(echo "$body" | python3 -c "
import sys,json
d=json.load(sys.stdin)
if isinstance(d,list): print(len(d))
else:
    for v in d.values():
        if isinstance(v,list): print(len(v)); sys.exit()
    print(-1)
" 2>/dev/null)
  fi
  echo "${len:--1}"
}

json_get() { echo "$1" | python3 -c "import sys,json; d=json.load(sys.stdin); v=d.get('$2') or (d.get('data',{}).get('$2') if isinstance(d.get('data'),dict) else None); print(v or '')" 2>/dev/null; }

echo "═══════════════════════════════════════════════"
echo "  仙途 API 自动化测试"
echo "  Base URL: $BASE_URL"
echo "  Time:     $(date '+%Y-%m-%d %H:%M:%S')"
echo "═══════════════════════════════════════════════"
echo

# ── 1. Health (no /health endpoint; use /api/world/status as proxy) ──
echo "── 基础健康检查 ──"
resp=$(api GET /api/world/status)
code=$(parse_status "$resp")
[[ "$code" == "200" ]] && pass "Health Check" "status $code (via /api/world/status)" || fail "Health Check" "status $code"
echo

# ── 2. World Status ──
echo "── 世界状态 ──"
resp=$(api GET /api/world/status)
code=$(parse_status "$resp")
body=$(parse_body "$resp")
if [[ "$code" == "200" ]]; then
  missing=""
  for key in current_year next_tribulation_year tribulation_element; do
    val=$(json_get "$body" "$key")
    [[ -z "$val" ]] && missing="$missing $key"
  done
  [[ -z "$missing" ]] && pass "World Status" "all required fields present" || fail "World Status" "missing:$missing"
else
  fail "World Status" "status $code"
fi
echo

# ── 3. Static Data ──
echo "── 静态数据完整性 ──"
check_static() {
  local name="$1" path="$2" min="$3"
  resp=$(api GET "$path")
  code=$(parse_status "$resp")
  body=$(parse_body "$resp")
  if [[ "$code" != "200" ]]; then fail "$name" "status $code"; return; fi
  local n
  n=$(json_array_len "$body")
  if (( n >= min )); then
    pass "$name" "count=$n (expected>=$min)"
  else
    fail "$name" "count=$n (expected>=$min)"
  fi
}

check_static "Realms (境界)"       /api/realms      9
check_static "Races (族裔)"        /api/races        6
check_static "Caves (洞府)"        /api/caves        30
check_static "City Realms (城市秘境)" /api/city-realms 30
check_static "Factions (门派)"     /api/factions     11
echo

# ── 4. Auth Flow ──
echo "── 玩家注册+登录 ──"
RAND_USER="test_$(cat /dev/urandom | LC_ALL=C tr -dc 'a-z0-9' | head -c10)"
PASSWORD="Test@12345"
TOKEN=""

resp=$(api POST /api/register "{\"username\":\"$RAND_USER\",\"password\":\"$PASSWORD\"}")
code=$(parse_status "$resp")
body=$(parse_body "$resp")
[[ "$code" == "200" || "$code" == "201" ]] && pass "Register" "status $code user=$RAND_USER" || fail "Register" "status $code"

resp=$(api POST /api/login "{\"username\":\"$RAND_USER\",\"password\":\"$PASSWORD\"}")
code=$(parse_status "$resp")
body=$(parse_body "$resp")
TOKEN=$(json_get "$body" "token")
if [[ "$code" == "200" && -n "$TOKEN" ]]; then
  pass "Login" "status $code token=true"
else
  fail "Login" "status $code token=${TOKEN:+true}"
fi

resp=$(api GET /api/profile "" "$TOKEN")
code=$(parse_status "$resp")
[[ "$code" == "200" ]] && pass "Character Info (profile)" "status $code" || fail "Character Info (profile)" "status $code"
echo

# ── 5. Core ──
echo "── 核心功能 ──"
if [[ -z "$TOKEN" ]]; then
  printf "${YELLOW}⚠ Skipping core tests — no auth token${RESET}\n"
else
  resp=$(api POST /api/cultivate/offline "" "$TOKEN")
  code=$(parse_status "$resp")
  [[ "$code" == "200" ]] && pass "Offline Cultivate" "status $code" || fail "Offline Cultivate" "status $code"

  resp=$(api GET /api/world/tribulation "" "$TOKEN")
  code=$(parse_status "$resp")
  [[ "$code" == "200" ]] && pass "Tribulation Progress" "status $code" || fail "Tribulation Progress" "status $code"

  resp=$(api GET /api/wishes/fulfilled "" "$TOKEN")
  code=$(parse_status "$resp")
  [[ "$code" == "200" ]] && pass "Wishes Fulfilled" "status $code" || fail "Wishes Fulfilled" "status $code"
fi
echo

# ── Summary ──
TOTAL=$((PASSED + FAILED))
echo "═══════════════════════════════════════════════"
printf "  Total: %d  |  ${GREEN}Passed: %d${RESET}  |  ${RED}Failed: %d${RESET}\n" "$TOTAL" "$PASSED" "$FAILED"
echo "═══════════════════════════════════════════════"

[[ "$FAILED" -gt 0 ]] && exit 1 || exit 0
