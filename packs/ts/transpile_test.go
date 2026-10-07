package ts

import (
	"strings"
	"testing"
)

func TestTranspile(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		contains []string
		omits    []string
	}{
		{
			name: "strip interfaces",
			input: `interface User {
	id: number;
	name: string;
}
console.log("hello");`,
			contains: []string{`console.log("hello");`},
			omits:    []string{"interface User", "name: string"},
		},
		{
			name: "strip type aliases",
			input: `type StringOrNum = string | number;
export type ID = string;
const x = 42;`,
			contains: []string{"const x = 42;"},
			omits:    []string{"type StringOrNum", "type ID"},
		},
		{
			name: "strip type annotations and return types",
			input: `function add(a: number, b: number): number {
	return a + b;
}
const greet = (name: string): void => {
	console.log("hi " + name);
};`,
			contains: []string{
				"function add(a, b)",
				"return a + b;",
				"const greet = (name) =>",
			},
			omits: []string{": number", ": void"},
		},
		{
			name: "convert enum to object",
			input: `enum Status {
	Pending,
	Active,
	Done,
}
console.log(Status.Active);`,
			contains: []string{
				"const Status = { Pending: 0, Active: 1, Done: 2 };",
				"console.log(Status.Active);",
			},
			omits: []string{"enum Status"},
		},
		{
			name: "generic functions",
			input: `function identity<T>(val: T): T {
	return val;
}
console.log(identity(123));`,
			contains: []string{
				"function identity(val)",
				"return val;",
				"console.log(identity(123));",
			},
			omits: []string{"<T>", ": T"},
		},
		{
			name: "type cast as",
			input: `const val = (raw as string).trim();`,
			contains: []string{
				"(raw).trim()",
			},
			omits: []string{"as string"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Transpile(tt.input)
			for _, c := range tt.contains {
				if !strings.Contains(got, c) {
					t.Errorf("Transpile() output missing expected %q\nGot:\n%s", c, got)
				}
			}
			for _, o := range tt.omits {
				if strings.Contains(got, o) {
					t.Errorf("Transpile() output unexpectedly contains %q\nGot:\n%s", o, got)
				}
			}
		})
	}
}
