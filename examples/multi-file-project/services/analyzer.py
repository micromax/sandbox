import sqlparse
from config import APP_NAME, VERSION

def analyze_query(sql_text: str) -> dict:
    parsed = sqlparse.parse(sql_text)
    formatted = sqlparse.format(sql_text, reindent=True, keyword_case='upper')
    return {
        "engine": f"{APP_NAME} v{VERSION}",
        "statement_count": len(parsed),
        "formatted": formatted,
    }
