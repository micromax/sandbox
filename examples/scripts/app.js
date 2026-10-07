// JavaScript Example - Runs in Pure WebAssembly (Zero Docker)
const fibonacci = (n) => {
    const seq = [0, 1];
    for (let i = 2; i < n; i++) {
        seq.push(seq[i - 1] + seq[i - 2]);
    }
    return seq;
};

const payload = {
    runtime: "QuickJS-NG (Wasm/WASI)",
    version: "ES2023",
    timestamp: new Date().toISOString(),
    fibonacci_10: fibonacci(10),
};

console.log("=== JavaScript (Pure Wasm) ===");
console.log("Calculated Fibonacci:", JSON.stringify(payload.fibonacci_10));
console.log("Runtime Info:", payload.runtime);
console.log("Status: OK");
