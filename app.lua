-- Lua Example - Runs in Pure WebAssembly (Zero Docker)
function factorial(n)
    if n <= 1 then return 1 end
    return n * factorial(n - 1)
end

local languages = {"JavaScript", "TypeScript", "Python", "Lua", "WASI"}

print("=== Lua 5.4 (Pure Wasm) ===")
print("Version         : " .. _VERSION)
print("Factorial of 7  : " .. factorial(7))
print("Tier 1 Wasm Languages:")
for i, lang in ipairs(languages) do
    print(string.format("  [%d] %s", i, lang))
end
print("Status: OK")
