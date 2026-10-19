package authz

import (
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/open-policy-agent/opa/v1/ast"
)

// Diagnostic is one problem in a policy source, with the place to highlight; rows and columns count from 1 and End is exclusive.
type Diagnostic struct {
	Severity string `json:"severity"`
	Message  string `json:"message"`
	Row      int    `json:"row"`
	Col      int    `json:"col"`
	EndRow   int    `json:"endRow"`
	EndCol   int    `json:"endCol"`
}

const severityError = "error"

// inputSchema is the shape of input; a policy that reads a field outside it (input.usr) is reported.
const inputSchema = `{
  "type": "object",
  "properties": {
    "resource": {"type": "string"},
    "userRoles": {"type": "array", "items": {"type": "string"}},
    "credentials": {"type": "object", "additionalProperties": {"type": "object", "properties": {
      "issuer": {"type": "string"},
      "claims": {"type": "object", "additionalProperties": {"type": "string"}}
    }}}
  }
}`

// Diagnose returns every problem in source that Validate would refuse it for, each with its place; none means the policy is accepted.
func (e *OPA) Diagnose(source string) []Diagnostic {
	module, err := ast.ParseModuleWithOpts("policy.rego", source, ast.ParserOptions{RegoVersion: ast.RegoV1})
	if err != nil {
		return fromErrors(err)
	}

	var found []Diagnostic
	if module.Package.Path.String() != "data."+policyPackage {
		found = append(found, at(module.Package.Location, "the policy must be in package "+policyPackage))
	}
	var decision *ast.Rule
	for _, r := range module.Rules {
		if r.Head.Name.String() == decisionRule {
			decision = r
			break
		}
	}
	if decision == nil {
		found = append(found, at(module.Package.Location, "the policy defines no rule "+decisionRule))
	}

	var schema any
	if err := json.Unmarshal([]byte(inputSchema), &schema); err != nil {
		panic(err)
	}
	schemas := ast.NewSchemaSet()
	schemas.Put(ast.SchemaRootRef, schema)
	compiler := ast.NewCompiler().WithCapabilities(e.capabilities).WithSchemas(schemas).WithStrict(true)
	compiler.Compile(map[string]*ast.Module{"policy.rego": module})
	if compiler.Failed() {
		found = append(found, fromErrors(compiler.Errors)...)
	}
	if len(found) > 0 {
		return found
	}

	// What is left cannot be placed by the compiler: the rule evaluates to a value of the wrong shape.
	if err := e.Validate(source); err != nil {
		return []Diagnostic{at(decision.Location, err.Error())}
	}
	return nil
}

func fromErrors(err error) []Diagnostic {
	var errs ast.Errors
	if !errors.As(err, &errs) {
		return []Diagnostic{at(nil, err.Error())}
	}
	out := make([]Diagnostic, 0, len(errs))
	for _, e := range errs {
		out = append(out, at(e.Location, e.Message))
	}
	return out
}

// at highlights the first line of the text at loc; without a location it is the first character.
func at(loc *ast.Location, message string) Diagnostic {
	d := Diagnostic{Severity: severityError, Message: message, Row: 1, Col: 1, EndRow: 1, EndCol: 2}
	if loc == nil || loc.Row < 1 {
		return d
	}
	first, _, _ := strings.Cut(string(loc.Text), "\n")
	first = strings.TrimRight(first, "\r")
	d.Row, d.Col, d.EndRow = loc.Row, max(loc.Col, 1), loc.Row
	d.EndCol = d.Col + max(utf8.RuneCountInString(first), 1)
	return d
}
