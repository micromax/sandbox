import sqlparse

raw_sql = """
select u.id, u.username, count(o.id) as order_count, sum(o.total_amount) as total_spent
from users u
left join orders o on o.user_id = u.id
where u.status = 'active' and o.created_at >= '2026-01-01'
group by u.id, u.username
having count(o.id) > 5
order by total_spent desc
limit 50;
"""

print("=== Raw SQL ===")
print(raw_sql.strip())

formatted = sqlparse.format(
    raw_sql,
    reindent=True,
    keyword_case="upper",
    indent_width=2
)
print("\n=== Formatted SQL via sqlparse in Sandbox ===")
print(formatted)

parsed = sqlparse.parse(raw_sql)
stmt = parsed[0]
print("\n=== Statement Type ===")
print("Type:", stmt.get_type())
print("Tokens Count:", len(stmt.tokens))
print("\nStatus: SUCCESS - Real-world sqlparse package executed in pure WebAssembly!")
