// Package main — xiantu API automated test runner.
//
// Usage:
//
//	go run ./test/                                              # default: production
//	BASE_URL=http://localhost:3000 go run ./test/               # local
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"strings"
	"time"
)

// ── colours ──────────────────────────────────────────────────────────────────

const (
	green  = "\033[32m"
	red    = "\033[31m"
	yellow = "\033[33m"
	reset  = "\033[0m"
)

// ── result tracking ──────────────────────────────────────────────────────────

type result struct {
	name   string
	pass   bool
	detail string
}

var results []result

func record(name string, pass bool, detail string) {
	tag := green + "✓ PASS" + reset
	if !pass {
		tag = red + "✗ FAIL" + reset
	}
	fmt.Printf("[%s] %s — %s\n", tag, name, detail)
	results = append(results, result{name, pass, detail})
}

// ── helpers ──────────────────────────────────────────────────────────────────

func baseURL() string {
	u := os.Getenv("BASE_URL")
	if u == "" {
		u = "https://xiantu-server-production.up.railway.app"
	}
	return strings.TrimRight(u, "/")
}

func get(path string, token string) (int, map[string]any, error) {
	req, _ := http.NewRequest("GET", baseURL()+path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return doReq(req)
}

func post(path string, body map[string]any, token string) (int, map[string]any, error) {
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", baseURL()+path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return doReq(req)
}

func doReq(req *http.Request) (int, map[string]any, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	return resp.StatusCode, m, nil
}

func randUsername() string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 10)
	for i := range b {
		b[i] = chars[rand.Intn(len(chars))]
	}
	return "test_" + string(b)
}

// ── assertion helpers ────────────────────────────────────────────────────────

func hasKeys(m map[string]any, keys ...string) []string {
	var missing []string
	for _, k := range keys {
		if _, ok := m[k]; !ok {
			missing = append(missing, k)
		}
	}
	return missing
}

func arrayLen(m map[string]any, key string) int {
	v, ok := m[key]
	if !ok {
		// maybe the response itself is a wrapper with "data"
		if d, ok2 := m["data"]; ok2 {
			if dm, ok3 := d.(map[string]any); ok3 {
				v = dm[key]
			}
		}
	}
	if v == nil {
		return -1
	}
	if arr, ok := v.([]any); ok {
		return len(arr)
	}
	return -1
}

// try to find the array in the response — it might be top-level, under "data", or the response itself
func findArray(m map[string]any) ([]any, bool) {
	// response is a wrapper with a known list key
	for _, k := range []string{"data", "realms", "races", "caves", "city_realms", "cityRealms", "factions", "items"} {
		if v, ok := m[k]; ok {
			if arr, ok2 := v.([]any); ok2 {
				return arr, true
			}
		}
	}
	return nil, false
}

func topLevelArrayLen(status int, m map[string]any) int {
	arr, ok := findArray(m)
	if ok {
		return len(arr)
	}
	return -1
}

// ── tests ────────────────────────────────────────────────────────────────────

func testHealth() {
	// No /health endpoint; use /api/world/status as a proxy health check
	code, _, err := get("/api/world/status", "")
	if err != nil {
		record("Health Check", false, err.Error())
		return
	}
	record("Health Check", code == 200, fmt.Sprintf("status %d (via /api/world/status)", code))
}

func testWorldStatus() {
	code, m, err := get("/api/world/status", "")
	if err != nil {
		record("World Status", false, err.Error())
		return
	}
	if code != 200 {
		record("World Status", false, fmt.Sprintf("status %d", code))
		return
	}
	missing := hasKeys(m, "current_year", "next_tribulation_year", "tribulation_element")
	if len(missing) > 0 {
		// check nested "data"
		if d, ok := m["data"].(map[string]any); ok {
			missing = hasKeys(d, "current_year", "next_tribulation_year", "tribulation_element")
		}
	}
	if len(missing) > 0 {
		record("World Status", false, fmt.Sprintf("missing keys: %v", missing))
	} else {
		record("World Status", true, "all required fields present")
	}
}

func testStaticData(name, path string, minCount int) {
	code, m, err := get(path, "")
	if err != nil {
		record(name, false, err.Error())
		return
	}
	if code != 200 {
		record(name, false, fmt.Sprintf("status %d", code))
		return
	}
	n := topLevelArrayLen(code, m)
	if n < 0 {
		record(name, false, "response is not a recognisable array")
		return
	}
	pass := n >= minCount
	record(name, pass, fmt.Sprintf("count=%d (expected>=%d)", n, minCount))
}

func testAuthFlow() (token string) {
	username := randUsername()
	password := "Test@12345"

	// register
	code, m, err := post("/api/register", map[string]any{
		"username": username,
		"password": password,
	}, "")
	if err != nil {
		record("Register", false, err.Error())
		return
	}
	record("Register", code == 200 || code == 201, fmt.Sprintf("status %d user=%s", code, username))

	// login
	code, m, err = post("/api/login", map[string]any{
		"username": username,
		"password": password,
	}, "")
	if err != nil {
		record("Login", false, err.Error())
		return
	}
	// extract token
	if t, ok := m["token"].(string); ok {
		token = t
	} else if d, ok := m["data"].(map[string]any); ok {
		if t2, ok2 := d["token"].(string); ok2 {
			token = t2
		}
	}
	record("Login", code == 200 && token != "", fmt.Sprintf("status %d token=%v", code, token != ""))

	// character info (profile)
	code, _, err = get("/api/profile", token)
	if err != nil {
		record("Character Info", false, err.Error())
		return
	}
	record("Character Info", code == 200, fmt.Sprintf("status %d", code))
	return
}

func testCore(token string) {
	// offline cultivate
	code, _, err := post("/api/cultivate/offline", nil, token)
	if err != nil {
		record("Offline Cultivate", false, err.Error())
	} else {
		record("Offline Cultivate", code == 200, fmt.Sprintf("status %d", code))
	}

	// tribulation
	code, _, err = get("/api/world/tribulation", token)
	if err != nil {
		record("Tribulation Progress", false, err.Error())
	} else {
		record("Tribulation Progress", code == 200, fmt.Sprintf("status %d", code))
	}

	// wishes
	code, _, err = get("/api/wishes/fulfilled", token)
	if err != nil {
		record("Wishes Fulfilled", false, err.Error())
	} else {
		record("Wishes Fulfilled", code == 200, fmt.Sprintf("status %d", code))
	}
}

// ── main ─────────────────────────────────────────────────────────────────────

func main() {
	rand.Seed(time.Now().UnixNano())

	fmt.Println("═══════════════════════════════════════════════")
	fmt.Println("  仙途 API 自动化测试")
	fmt.Printf("  Base URL: %s\n", baseURL())
	fmt.Printf("  Time:     %s\n", time.Now().Format("2006-01-02 15:04:05"))
	fmt.Println("═══════════════════════════════════════════════")
	fmt.Println()

	// 1. Health
	fmt.Println("── 基础健康检查 ──")
	testHealth()
	fmt.Println()

	// 2. World status
	fmt.Println("── 世界状态 ──")
	testWorldStatus()
	fmt.Println()

	// 3. Static data
	fmt.Println("── 静态数据完整性 ──")
	testStaticData("Realms (境界)", "/api/realms", 9)
	testStaticData("Races (族裔)", "/api/races", 6)
	testStaticData("Caves (洞府)", "/api/caves", 30)
	testStaticData("City Realms (城市秘境)", "/api/city-realms", 30)
	testStaticData("Factions (门派)", "/api/factions", 11)
	fmt.Println()

	// 4. Auth flow
	fmt.Println("── 玩家注册+登录 ──")
	token := testAuthFlow()
	fmt.Println()

	// 5. Core
	fmt.Println("── 核心功能 ──")
	if token == "" {
		fmt.Println(yellow + "⚠ Skipping core tests — no auth token" + reset)
	} else {
		testCore(token)
	}
	fmt.Println()

	// Summary
	passed, failed := 0, 0
	for _, r := range results {
		if r.pass {
			passed++
		} else {
			failed++
		}
	}
	total := passed + failed
	fmt.Println("═══════════════════════════════════════════════")
	fmt.Printf("  Total: %d  |  %sPassed: %d%s  |  %sFailed: %d%s\n",
		total, green, passed, reset, red, failed, reset)
	fmt.Println("═══════════════════════════════════════════════")

	if failed > 0 {
		os.Exit(1)
	}
}
