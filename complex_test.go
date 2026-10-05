package sandbox_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/micromax/sandbox"
	"github.com/micromax/sandbox/backend/wasm"
	"github.com/micromax/sandbox/packs/js"
	"github.com/micromax/sandbox/packs/python"
)

// TestComplexScenario_PythonMultiFileAnalytics tests an end-to-end multi-file
// data analysis pipeline running inside the Python Wasm sandbox.
func TestComplexScenario_PythonMultiFileAnalytics(t *testing.T) {
	wBackend, err := wasm.New()
	if err != nil {
		t.Fatalf("wasm.New: %v", err)
	}

	pyPack := python.Pack()
	sb, err := sandbox.New(
		sandbox.WithBackends(wBackend),
		sandbox.WithPacks(pyPack),
		sandbox.WithPolicy(sandbox.WasmOnly),
	)
	if err != nil {
		t.Fatalf("sandbox.New: %v", err)
	}

	// 1. Generate 1,000 synthetic transaction rows
	var csvRows []string
	csvRows = append(csvRows, "tx_id,customer_id,category,amount,quantity")
	for i := 1; i <= 1000; i++ {
		cat := "Electronics"
		if i%3 == 0 {
			cat = "Books"
		} else if i%3 == 1 {
			cat = "Apparel"
		}
		amount := float64((i*17)%500) + 9.99
		qty := (i % 5) + 1
		csvRows = append(csvRows, fmt.Sprintf("TX-%04d,CUST-%03d,%s,%.2f,%d", i, (i%50)+1, cat, amount, qty))
	}
	transactionsCSV := strings.Join(csvRows, "\n")

	// 2. Tax rules JSON
	taxRulesJSON := `{
		"Electronics": 0.18,
		"Apparel": 0.08,
		"Books": 0.00
	}`

	// 3. Submodule: lib/stats.py
	statsPy := `
import math

def calculate_stats(values):
    if not values:
        return {"count": 0, "sum": 0, "mean": 0, "std_dev": 0, "min": 0, "max": 0}
    n = len(values)
    s = sum(values)
    mean = s / n
    variance = sum((x - mean) ** 2 for x in values) / n
    return {
        "count": n,
        "sum": round(s, 2),
        "mean": round(mean, 2),
        "std_dev": round(math.sqrt(variance), 2),
        "min": round(min(values), 2),
        "max": round(max(values), 2)
    }
`

	// 4. Submodule: models/record.py
	recordPy := `
class Transaction:
    def __init__(self, tx_id, customer_id, category, amount, quantity):
        self.tx_id = tx_id
        self.customer_id = customer_id
        self.category = category
        self.amount = float(amount)
        self.quantity = int(quantity)

    @classmethod
    def from_csv_line(cls, line):
        parts = line.strip().split(',')
        return cls(parts[0], parts[1], parts[2], parts[3], parts[4])
`

	// 5. Entry point: main.py
	mainPy := `
import sys
import os
import json
import csv

# Add /in to Python module search path
sys.path.insert(0, '/in')

from lib.stats import calculate_stats
from models.record import Transaction

# Read tax rules
with open('/in/config/tax_rules.json', 'r') as f:
    tax_rules = json.load(f)

# Read CSV transactions
transactions = []
with open('/in/data/transactions.csv', 'r') as f:
    header = f.readline()
    for line in f:
        line = line.strip()
        if line:
            transactions.append(Transaction.from_csv_line(line))

print(f"Loaded {len(transactions)} transactions.")

# Group by category and compute totals
by_category = {}
customer_spend = {}

for tx in transactions:
    # Tax calculation
    tax_rate = tax_rules.get(tx.category, 0.05)
    tax_amount = round(tx.amount * tx.quantity * tax_rate, 2)
    net_total = round(tx.amount * tx.quantity + tax_amount, 2)

    # Category aggregation
    if tx.category not in by_category:
        by_category[tx.category] = []
    by_category[tx.category].append(net_total)

    # Customer aggregation
    customer_spend[tx.customer_id] = customer_spend.get(tx.customer_id, 0.0) + net_total

# Generate KPIs JSON
kpis = {
    "total_transactions": len(transactions),
    "total_customers": len(customer_spend),
    "categories": {}
}

for cat, amounts in by_category.items():
    kpis["categories"][cat] = calculate_stats(amounts)

# Write output 1: KPIs JSON
os.makedirs('/out', exist_ok=True)
with open('/out/kpis.json', 'w') as f:
    json.dump(kpis, f, indent=2)

# Write output 2: Top 10 Customers CSV
top_customers = sorted(customer_spend.items(), key=lambda x: x[1], reverse=True)[:10]
with open('/out/top_customers.csv', 'w') as f:
    f.write("customer_id,total_spent\n")
    for cid, spent in top_customers:
        f.write(f"{cid},{spent:.2f}\n")

# Write output 3: Executive Brief Text
with open('/out/executive_brief.txt', 'w') as f:
    f.write("=== EXECUTIVE SALES BRIEF ===\n")
    f.write(f"Processed Transactions: {len(transactions)}\n")
    for cat, stats in kpis["categories"].items():
        f.write(f"Category {cat}: Total ${stats['sum']:,.2f} (Mean: ${stats['mean']})\n")
    f.write(f"\nTop Spender: {top_customers[0][0]} with ${top_customers[0][1]:,.2f}\n")

print("Processing complete. Generated kpis.json, top_customers.csv, executive_brief.txt.")
`

	files := map[string][]byte{
		"config/tax_rules.json": []byte(taxRulesJSON),
		"data/transactions.csv": []byte(transactionsCSV),
		"lib/stats.py":          []byte(statsPy),
		"models/record.py":      []byte(recordPy),
	}

	start := time.Now()
	res, err := sb.Run(context.Background(), sandbox.Spec{
		Lang:  "python",
		Code:  mainPy,
		Files: files,
		Limits: &sandbox.Limits{
			WallTime: 15 * time.Second,
			Memory:   128 * 1024 * 1024, // 128 MB
		},
	})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("sb.Run failed: %v\nStderr: %s", err, string(res.Stderr))
	}

	t.Logf("Execution completed in %v (Peak Memory: %d bytes)", elapsed, res.Usage.PeakMemory)
	t.Logf("Stdout output:\n%s", string(res.Stdout))

	// Verify all 3 output files were generated in /out
	if len(res.Files) != 3 {
		t.Fatalf("expected 3 files in /out, got %d: %v", len(res.Files), res.Files)
	}

	// 1. Verify kpis.json
	kpiData, ok := res.Files["kpis.json"]
	if !ok {
		t.Fatal("missing kpis.json in output")
	}
	var kpiObj struct {
		TotalTransactions int `json:"total_transactions"`
		TotalCustomers    int `json:"total_customers"`
		Categories        map[string]struct {
			Count int     `json:"count"`
			Sum   float64 `json:"sum"`
			Mean  float64 `json:"mean"`
		} `json:"categories"`
	}
	if err := json.Unmarshal(kpiData, &kpiObj); err != nil {
		t.Fatalf("unmarshal kpis.json: %v", err)
	}
	if kpiObj.TotalTransactions != 1000 {
		t.Errorf("expected 1000 transactions, got %d", kpiObj.TotalTransactions)
	}
	if kpiObj.TotalCustomers != 50 {
		t.Errorf("expected 50 customers, got %d", kpiObj.TotalCustomers)
	}
	t.Logf("Verified KPIs: %d transactions across %d customers", kpiObj.TotalTransactions, kpiObj.TotalCustomers)

	// 2. Verify top_customers.csv
	topCustData, ok := res.Files["top_customers.csv"]
	if !ok {
		t.Fatal("missing top_customers.csv in output")
	}
	lines := strings.Split(strings.TrimSpace(string(topCustData)), "\n")
	if len(lines) != 11 { // 1 header + 10 rows
		t.Errorf("expected 11 lines in top_customers.csv, got %d", len(lines))
	}

	// 3. Verify executive_brief.txt
	briefData, ok := res.Files["executive_brief.txt"]
	if !ok {
		t.Fatal("missing executive_brief.txt in output")
	}
	if !strings.Contains(string(briefData), "EXECUTIVE SALES BRIEF") {
		t.Errorf("expected brief header in executive_brief.txt")
	}
	t.Logf("Executive brief:\n%s", string(briefData))
}

// TestComplexScenario_JavaScriptMultiFileServe tests a multi-file JavaScript project
// that serves HTTP requests with path-based routing, database lookup, and Bearer auth.
func TestComplexScenario_JavaScriptMultiFileServe(t *testing.T) {
	wBackend, err := wasm.New()
	if err != nil {
		t.Fatalf("wasm.New: %v", err)
	}

	jsPack := js.Pack()
	sb, err := sandbox.New(
		sandbox.WithBackends(wBackend),
		sandbox.WithPacks(jsPack),
		sandbox.WithPolicy(sandbox.WasmOnly),
	)
	if err != nil {
		t.Fatalf("sandbox.New: %v", err)
	}

	// Multi-file JavaScript app:
	// db/data.json: Seed users
	dbJSON := `[
		{"id": 1, "name": "Alice", "role": "admin", "balance": 1500.0},
		{"id": 2, "name": "Bob", "role": "developer", "balance": 240.50},
		{"id": 3, "name": "Charlie", "role": "designer", "balance": 820.75}
	]`

	// Main service code with router
	mainJS := `
// Load seed data from /in/db/data.json
var users = JSON.parse(std.loadFile('/in/db/data.json'));

function fetch(req) {
    var path = req.path || "/";

    if (path === "/health") {
        return { status: 200, headers: {"Content-Type": "text/plain"}, body: "OK" };
    }

    if (path === "/api/users") {
        if (req.method === "GET") {
            return {
                status: 200,
                headers: {"Content-Type": "application/json"},
                body: JSON.stringify({ count: users.length, users: users })
            };
        } else if (req.method === "POST") {
            try {
                var newUser = JSON.parse(req.body);
                newUser.id = users.length + 1;
                users.push(newUser);
                return {
                    status: 201,
                    headers: {"Content-Type": "application/json"},
                    body: JSON.stringify({ success: true, user: newUser })
                };
            } catch (e) {
                return { status: 400, body: "Invalid JSON: " + e };
            }
        }
    }

    if (path.startsWith("/api/users/")) {
        var id = parseInt(path.substring("/api/users/".length));
        var user = users.find(function(u) { return u.id === id; });
        if (user) {
            return {
                status: 200,
                headers: {"Content-Type": "application/json"},
                body: JSON.stringify(user)
            };
        }
        return { status: 404, headers: {"Content-Type": "text/plain"}, body: "User Not Found" };
    }

    return { status: 404, body: "Route Not Found" };
}
`

	files := map[string][]byte{
		"db/data.json": []byte(dbJSON),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	svc, err := sb.Serve(ctx, sandbox.Spec{
		Lang:  "js",
		Code:  mainJS,
		Files: files,
	}, sandbox.ServeOpts{
		Ports: []sandbox.PortMap{{Guest: 8080}},
		Bind:  sandbox.Loopback,
		Auth:  sandbox.BearerToken("master-secret-key"),
		Ready: sandbox.HTTPReady("/health", 5*time.Second),
	})
	if err != nil {
		t.Fatalf("sb.Serve: %v", err)
	}
	defer svc.Stop()

	client := &http.Client{Timeout: 3 * time.Second}
	doReq := func(method, path string, body string) (*http.Response, string) {
		req, _ := http.NewRequest(method, svc.URL()+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+svc.Token())
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("client.Do %s %s: %v", method, path, err)
		}
		defer resp.Body.Close()
		respBytes, _ := io.ReadAll(resp.Body)
		return resp, string(respBytes)
	}

	// 1. GET /api/users -> initial 3 users
	resp, body := doReq(http.MethodGet, "/api/users", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/users expected 200, got %d: %s", resp.StatusCode, body)
	}
	if !strings.Contains(body, "Alice") || !strings.Contains(body, "Bob") {
		t.Errorf("expected initial users in body, got: %s", body)
	}

	// 2. POST /api/users -> add new user Diana
	resp, body = doReq(http.MethodPost, "/api/users", `{"name": "Diana", "role": "manager", "balance": 999.0}`)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST /api/users expected 201, got %d: %s", resp.StatusCode, body)
	}
	if !strings.Contains(body, "Diana") || !strings.Contains(body, `"id":4`) {
		t.Errorf("expected user Diana with id 4, got: %s", body)
	}

	// 3. GET /api/users/4 -> verify state persistence across requests!
	resp, body = doReq(http.MethodGet, "/api/users/4", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/users/4 expected 200, got %d: %s", resp.StatusCode, body)
	}
	if !strings.Contains(body, "Diana") {
		t.Errorf("expected user Diana at id 4, got: %s", body)
	}

	// 4. GET /api/users/999 -> 404 Not Found
	resp, _ = doReq(http.MethodGet, "/api/users/999", "")
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 for unknown user, got: %d", resp.StatusCode)
	}

	t.Log("Successfully verified multi-file JS service with in-memory state mutation and Bearer auth!")
}

// TestComplexScenario_AdversarialSecurityAndLimits stresses the sandbox with
// hostile paths, memory flooding, output spamming, and infinite loops.
func TestComplexScenario_AdversarialSecurityAndLimits(t *testing.T) {
	wBackend, err := wasm.New()
	if err != nil {
		t.Fatalf("wasm.New: %v", err)
	}

	jsPack := js.Pack()
	sb, err := sandbox.New(
		sandbox.WithBackends(wBackend),
		sandbox.WithPacks(jsPack),
		sandbox.WithPolicy(sandbox.WasmOnly),
	)
	if err != nil {
		t.Fatalf("sandbox.New: %v", err)
	}

	ctx := context.Background()

	// 1. Hostile Path Traversal in Spec.Files
	t.Run("PathTraversalRejected", func(t *testing.T) {
		hostileFiles := map[string][]byte{
			"../../evil.sh": []byte("rm -rf /"),
		}
		_, err := sb.Run(ctx, sandbox.Spec{
			Lang:  "js",
			Code:  `console.log("should not run");`,
			Files: hostileFiles,
		})
		if err == nil {
			t.Fatal("expected error rejecting path traversal, got nil")
		}
		if !strings.Contains(err.Error(), "escapes base") && !strings.Contains(err.Error(), "invalid spec") {
			t.Errorf("unexpected error message: %v", err)
		}
		t.Logf("Hostile path traversal successfully blocked: %v", err)
	})

	// 2. Output Limit Protection (50 KB cap vs 10 MB print loop)
	t.Run("OutputSpamCapped", func(t *testing.T) {
		res, err := sb.Run(ctx, sandbox.Spec{
			Lang: "js",
			Code: `
var chunk = "0123456789ABCDEF".repeat(64); // 1 KB
for (var i = 0; i < 2000; i++) {
    console.log(chunk);
}
`,
			Limits: &sandbox.Limits{
				MaxOutput: 32 * 1024, // 32 KB limit
			},
		})
		if !errors.Is(err, sandbox.ErrOutputLimit) {
			t.Fatalf("expected ErrOutputLimit, got: %v", err)
		}
		if res == nil {
			t.Fatal("expected non-nil partial result")
		}
		if len(res.Stdout) > 33*1024 {
			t.Fatalf("stdout exceeded cap: %d bytes", len(res.Stdout))
		}
		t.Logf("Output flood capped cleanly at %d bytes", len(res.Stdout))
	})

	// 3. WallTime Timeout Protection
	t.Run("InfiniteLoopTerminated", func(t *testing.T) {
		start := time.Now()
		_, err := sb.Run(ctx, sandbox.Spec{
			Lang: "js",
			Code: `while (true) {}`,
			Limits: &sandbox.Limits{
				WallTime: 300 * time.Millisecond,
			},
		})
		elapsed := time.Since(start)
		if !errors.Is(err, sandbox.ErrTimeout) {
			t.Fatalf("expected ErrTimeout, got: %v", err)
		}
		if elapsed > 1500*time.Millisecond {
			t.Fatalf("timeout took too long to terminate: %v", elapsed)
		}
		t.Logf("Infinite loop terminated cleanly in %v", elapsed)
	})
}

