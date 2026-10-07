package ts

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

var (
	importTypeRe = regexp.MustCompile(`(?m)^\s*import\s+type\s+[^;]+;\s*`)
	typeAliasRe  = regexp.MustCompile(`(?m)^\s*(export\s+)?type\s+\w+(?:<[^>]+>)?\s*=\s*[^;]+;\s*`)
	genericFnRe  = regexp.MustCompile(`\b([A-Za-z0-9_]+)\s*<[A-Za-z0-9_,\s]+>\s*\(`)
	fnReturnRe   = regexp.MustCompile(`\)\s*:\s*[A-Za-z0-9_<>\[\]|&\s]+\s*([{=])`)
	varDeclRe    = regexp.MustCompile(`\b(let|const|var)\s+([A-Za-z0-9_]+)\s*:\s*[A-Za-z0-9_<>\[\]|&]+\s*(=|;)`)
	asCastRe     = regexp.MustCompile(`\s+as\s+[A-Za-z0-9_<>\[\]|&]+`)
	paramListRe  = regexp.MustCompile(`\(([^)]*)\)\s*([{=]|\s*=>)`)
)

// Transpile converts TypeScript source code into executable JavaScript
// by stripping type annotations, interfaces, type aliases, and enums.
func Transpile(src string) string {
	// 1. Remove import type statements
	res := importTypeRe.ReplaceAllString(src, "")

	// 2. Remove type aliases: type X = ...;
	res = typeAliasRe.ReplaceAllString(res, "")

	// 3. Remove interface declarations with brace matching
	res = stripInterfaces(res)

	// 4. Convert enums to const objects
	res = convertEnums(res)

	// 5. Remove generic parameters from function declarations: foo<T>(...) -> foo(...)
	res = genericFnRe.ReplaceAllString(res, "$1(")

	// 6. Remove return type annotations: ): Type { or ): Type =>
	res = fnReturnRe.ReplaceAllString(res, ") $1")

	// 7. Strip variable type annotations: const x: number = 42; -> const x = 42;
	res = varDeclRe.ReplaceAllString(res, "$1 $2 $3")

	// 8. Strip function parameter types: (a: number, b?: string) -> (a, b)
	res = paramListRe.ReplaceAllStringFunc(res, func(m string) string {
		idxOpen := strings.Index(m, "(")
		idxClose := strings.LastIndex(m, ")")
		if idxOpen == -1 || idxClose == -1 || idxClose <= idxOpen {
			return m
		}
		paramStr := m[idxOpen+1 : idxClose]
		suffix := m[idxClose:]

		strippedParams := stripParams(paramStr)
		return m[:idxOpen+1] + strippedParams + suffix
	})

	// 9. Remove type assertions: x as string -> x
	res = asCastRe.ReplaceAllString(res, "")

	return res
}

func stripParams(params string) string {
	if strings.TrimSpace(params) == "" {
		return params
	}
	parts := strings.Split(params, ",")
	var cleaned []string
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed == "" {
			continue
		}
		colonIdx := strings.Index(trimmed, ":")
		if colonIdx == -1 {
			cleaned = append(cleaned, trimmed)
			continue
		}

		namePart := strings.TrimSpace(trimmed[:colonIdx])
		namePart = strings.TrimSuffix(namePart, "?")

		valPart := ""
		rest := trimmed[colonIdx+1:]
		if eqIdx := strings.Index(rest, "="); eqIdx != -1 {
			valPart = " = " + strings.TrimSpace(rest[eqIdx+1:])
		}
		cleaned = append(cleaned, namePart+valPart)
	}
	return strings.Join(cleaned, ", ")
}

func stripInterfaces(src string) string {
	var sb strings.Builder
	runes := []rune(src)
	n := len(runes)
	i := 0

	for i < n {
		if isKeywordAt(runes, i, "interface") || isKeywordAt(runes, i, "export interface") {
			braceIdx := -1
			for j := i; j < n; j++ {
				if runes[j] == '{' {
					braceIdx = j
					break
				}
				if runes[j] == ';' || (runes[j] == '\n' && j > i+40) {
					break
				}
			}
			if braceIdx != -1 {
				depth := 1
				k := braceIdx + 1
				for k < n && depth > 0 {
					if runes[k] == '{' {
						depth++
					} else if runes[k] == '}' {
						depth--
					}
					k++
				}
				i = k
				continue
			}
		}
		sb.WriteRune(runes[i])
		i++
	}
	return sb.String()
}

func convertEnums(src string) string {
	var sb strings.Builder
	runes := []rune(src)
	n := len(runes)
	i := 0

	for i < n {
		if isKeywordAt(runes, i, "enum") || isKeywordAt(runes, i, "export enum") {
			prefix := "enum"
			if isKeywordAt(runes, i, "export enum") {
				prefix = "export enum"
			}
			startIdx := i + len(prefix)
			for startIdx < n && unicode.IsSpace(runes[startIdx]) {
				startIdx++
			}
			nameStart := startIdx
			for startIdx < n && (unicode.IsLetter(runes[startIdx]) || unicode.IsDigit(runes[startIdx]) || runes[startIdx] == '_') {
				startIdx++
			}
			enumName := string(runes[nameStart:startIdx])

			for startIdx < n && runes[startIdx] != '{' {
				startIdx++
			}
			if startIdx < n && runes[startIdx] == '{' {
				endIdx := startIdx + 1
				for endIdx < n && runes[endIdx] != '}' {
					endIdx++
				}
				if endIdx < n {
					body := string(runes[startIdx+1 : endIdx])
					sb.WriteString(transformEnumBody(enumName, body))
					i = endIdx + 1
					continue
				}
			}
		}
		sb.WriteRune(runes[i])
		i++
	}
	return sb.String()
}

func transformEnumBody(name, body string) string {
	parts := strings.Split(body, ",")
	var entries []string
	nextVal := 0

	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed == "" {
			continue
		}
		if strings.Contains(trimmed, "=") {
			kv := strings.SplitN(trimmed, "=", 2)
			k := strings.TrimSpace(kv[0])
			v := strings.TrimSpace(kv[1])
			if intV, err := strconv.Atoi(v); err == nil {
				nextVal = intV + 1
			}
			entries = append(entries, k+": "+v)
		} else {
			entries = append(entries, fmtKeyVal(trimmed, nextVal))
			nextVal++
		}
	}
	return "const " + name + " = { " + strings.Join(entries, ", ") + " };\n"
}

func fmtKeyVal(k string, v int) string {
	return strings.TrimSpace(k) + ": " + strconv.Itoa(v)
}

func isKeywordAt(runes []rune, i int, kw string) bool {
	kwRunes := []rune(kw)
	if i+len(kwRunes) > len(runes) {
		return false
	}
	for j := range kwRunes {
		if runes[i+j] != kwRunes[j] {
			return false
		}
	}
	if i > 0 && (unicode.IsLetter(runes[i-1]) || unicode.IsDigit(runes[i-1])) {
		return false
	}
	afterIdx := i + len(kwRunes)
	if afterIdx < len(runes) && (unicode.IsLetter(runes[afterIdx]) || unicode.IsDigit(runes[afterIdx])) {
		return false
	}
	return true
}
