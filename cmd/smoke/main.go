package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"datacurve-takehome/internal/qa"
)

type createResp struct {
	TraceID     string `json:"trace_id"`
	UploadToken string `json:"upload_token"`
}

type appendResp struct {
	Accepted int   `json:"accepted"`
	NextSeq  int64 `json:"next_seq"`
}

type getTraceResp struct {
	TraceID     string         `json:"trace_id"`
	Developer   map[string]any `json:"developer"`
	Task        map[string]any `json:"task"`
	Environment map[string]any `json:"environment"`
	Artifacts   map[string]any `json:"artifacts,omitempty"`
	QA          map[string]any `json:"qa,omitempty"`
	Version     string         `json:"version"`
	Extra       map[string]any `json:"-"`
}

func main() {
	base := envOr("API_BASE_URL", "http://localhost:8000")
	token := envOr("API_TOKEN", "dev-secret-token")

	baseFlag := flag.String("base", base, "API base URL (e.g., http://localhost:8000)")
	tokenFlag := flag.String("token", token, "API token for admin endpoints")
	waitQA := flag.Duration("wait-qa", 5*time.Second, "How long to poll for QA results after enqueue")
	testQARunner := flag.Bool("test-qa", false, "Test the QA runner directly with buggy_repo")
	flag.Parse()

	// If testing QA runner directly, skip the API calls
	if *testQARunner {
		testQARunnerDirectly()
		return
	}

	httpc := &http.Client{Timeout: 12 * time.Second}

	// 1) Create trace
	createBody := map[string]any{
		"developer": map[string]any{
			"name":       "Smoke Tester",
			"email":      "smoke@example.com",
			"experience": "senior",
		},
		"task": map[string]any{
			"description":  "Fix bug in calculator function",
			"repository":   "https://github.com/tigercxx/buggy_repo",
			"branch":       "main",
			"commit":       "9e454b2",
			"test_image":   "golang:1.24",
			"test_command": "go test ./...",
		},
		"environment": map[string]any{
			"os":      "linux",
			"editor":  "vscode",
			"version": "1.0.0",
		},
	}
	var created createResp
	if err := postJSON(httpc, *baseFlag+"/traces", *tokenFlag, createBody, &created); err != nil {
		fatalf("create trace: %v", err)
	}
	fmt.Printf("✅ Created trace: id=%s upload_token=%s\n", created.TraceID, created.UploadToken)

	// 2) Append events (with upload token)
	now := time.Now().UTC()
	events := []map[string]any{
		{
			"t":          now.Format(time.RFC3339),
			"type":       "file_opened",
			"session_id": "smoke-1",
			"file_path":  "main.go",
			"language":   "go",
		},
		{
			"t":             now.Add(1 * time.Second).Format(time.RFC3339),
			"type":          "edit",
			"session_id":    "smoke-1",
			"file_path":     "main.go",
			"op":            "replace",
			"patch_unified": "diff --git a/main.go b/main.go\nindex 6bf3771..9d21ff9 100644\n--- a/main.go\n+++ b/main.go\n@@ -36,7 +36,7 @@ func (c *Calculator) ComplexCalculation(x, y float64) float64 {\n 	// This function has a deliberate bug for testing purposes\n 	result := c.Add(x, y) * 2\n 	// Bug: should be + 1, but we're subtracting 1\n-	return result - 1\n+	return result + 1\n }\n \n // Power calculates x raised to the power of y\n",
		},
		{
			"t":          now.Add(2 * time.Second).Format(time.RFC3339),
			"type":       "terminal_command",
			"session_id": "smoke-1",
			"cmd":        "go test",
			"args":       []string{"-v"},
			"cwd":        "/repo",
			"exit_code":  0,
		},
	}
	var appended appendResp
	if err := postJSONWithUpload(httpc, fmt.Sprintf("%s/traces/%s/events", *baseFlag, created.TraceID), created.UploadToken, map[string]any{"events": events}, &appended); err != nil {
		fatalf("append events: %v", err)
	}
	fmt.Printf("✅ Appended events: accepted=%d next_seq=%d\n", appended.Accepted, appended.NextSeq)

	// 3) Finalize
	if err := postJSON(httpc, fmt.Sprintf("%s/traces/%s/finalize", *baseFlag, created.TraceID), *tokenFlag, nil, &map[string]any{}); err != nil {
		fatalf("finalize: %v", err)
	}
	fmt.Println("✅ Finalized trace")

	// 4) Enqueue QA
	if err := postJSON(httpc, fmt.Sprintf("%s/traces/%s/qa", *baseFlag, created.TraceID), *tokenFlag, nil, &map[string]any{}); err != nil {
		fatalf("enqueue QA: %v", err)
	}
	fmt.Println("✅ Enqueued QA")

	time.Sleep(10 * time.Second)
	// 5) Get trace (optionally poll for QA result)
	deadline := time.Now().Add(*waitQA)
	var tr getTraceResp
	for {
		if err := getJSON(httpc, fmt.Sprintf("%s/traces/%s", *baseFlag, created.TraceID), *tokenFlag, &tr); err != nil {
			fatalf("get trace: %v", err)
		}
		if len(tr.QA) > 0 {
			fmt.Printf("✅ QA present in trace: %v\n", compactJSON(tr.QA))
			break
		}
		if time.Now().After(deadline) {
			fmt.Printf("ℹ️  QA not present yet (this is OK with the stub). Current trace:\n%s\n", compactJSON(tr))
			break
		}
		time.Sleep(5 * time.Second)
	}

	fmt.Printf("🎉 Smoke run OK. TraceID=%s\n", created.TraceID)
}

// --- helpers ---

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func postJSON(c *http.Client, url, bearer string, body any, out any) error {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r = bytes.NewReader(b)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, url, r)
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	res, err := c.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		b, _ := io.ReadAll(res.Body)
		return fmt.Errorf("POST %s -> %d: %s", url, res.StatusCode, string(b))
	}
	if out != nil {
		return json.NewDecoder(res.Body).Decode(out)
	}
	return nil
}

func postJSONWithUpload(c *http.Client, url, uploadToken string, body any, out any) error {
	if uploadToken == "" {
		return errors.New("upload token required")
	}
	return postJSON(c, url, uploadToken, body, out)
}

func getJSON(c *http.Client, url, bearer string, out any) error {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	res, err := c.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode/100 != 2 {
		b, _ := io.ReadAll(res.Body)
		return fmt.Errorf("GET %s -> %d: %s", url, res.StatusCode, string(b))
	}
	return json.NewDecoder(res.Body).Decode(out)
}

func compactJSON(v any) string {
	b, _ := json.MarshalIndent(v, "", "  ")
	return string(b)
}

func fatalf(format string, args ...any) {
	fmt.Printf("❌ "+format+"\n", args...)
	os.Exit(1)
}

// testQARunnerDirectly tests the QA runner with the buggy_repo code
func testQARunnerDirectly() {
	ctx := context.Background()

	fmt.Println("🧪 Testing QA runner directly with buggy_repo...")

	// The patch to fix the bug in ComplexCalculation
	patch := `diff --git a/main.go b/main.go
index 6bf3771..9d21ff9 100644
--- a/main.go
+++ b/main.go
@@ -36,7 +36,7 @@ func (c *Calculator) ComplexCalculation(x, y float64) float64 {
 	// This function has a deliberate bug for testing purposes
 	result := c.Add(x, y) * 2
 	// Bug: should be + 1, but we're subtracting 1
-	return result - 1
+	return result + 1
 }
 
 // Power calculates x raised to the power of y
`

	// Run the tests using the QA runner
	result, err := qa.RunTests(ctx, "https://github.com/tigercxx/buggy_repo", "9e454b2", patch, "golang:1.24", "go test -v")
	if err != nil {
		fatalf("QA runner failed: %v", err)
	}

	// Print results
	fmt.Printf("\n📊 Test Results:\n")
	fmt.Printf("  ✅ OK: %t\n", result.OK)
	fmt.Printf("  🔢 Exit Code: %d\n", result.ExitCode)
	fmt.Printf("  📤 Stdout:\n%s\n", result.Stdout)
	if result.Stderr != "" {
		fmt.Printf("  📥 Stderr:\n%s\n", result.Stderr)
	}

	if result.OK {
		fmt.Println("🎉 All tests passed! The patch fixed the bug.")
	} else {
		fmt.Println("❌ Some tests failed. The patch may not have fixed all issues.")
	}
}
