# Python Example - Runs in Pure WebAssembly (Zero Docker)
import sys
import json
import math
from collections import Counter

def analyze_text(text: str):
    words = text.lower().split()
    counts = Counter(words)
    return {
        "word_count": len(words),
        "unique_words": len(counts),
        "top_word": counts.most_common(1)[0][0] if counts else None,
        "pi_approx": round(math.pi, 5)
    }

sample = "Sandbox provides pure Go WebAssembly isolation with zero Docker dependencies for sandbox execution"
stats = analyze_text(sample)

print("=== Python 3.13 (Pure Wasm) ===")
print(f"Python Version : {sys.version.split()[0]}")
print(f"Stats Result   : {json.dumps(stats, indent=2)}")
print("Status: OK")
