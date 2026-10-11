import json
from services.analyzer import analyze_query

raw_query = "select id, name, email from users where active = 1 order by id desc;"
result = analyze_query(raw_query)

print("=== MULTI-FILE PROJECT EXECUTION SUCCESS ===")
print(f"Engine: {result['engine']}")
print(f"Statements detected: {result['statement_count']}")
print("Formatted SQL:")
print(result['formatted'])
