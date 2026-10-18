package authz

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/rego"
)

const (
	policyPackage = "policy"
	decisionRule  = "decision"
	query         = "data." + policyPackage + "." + decisionRule

	// evalTimeout bounds one evaluation; a policy over a few claims takes microseconds.
	evalTimeout = 500 * time.Millisecond
	// maxPrepared bounds the compiled policies kept; at the limit the cache starts over.
	maxPrepared = 64

	reasonNoDecision = "Geen besluit mogelijk"
)

// nondeterministic or reaching outside the process: a policy may not call these.
var forbiddenBuiltins = map[string]bool{
	"http.send": true, "net.lookup_ip_addr": true, "opa.runtime": true, "time.now_ns": true, "rand.intn": true, "uuid.rfc4122": true,
}

// OPA is an Evaluator backed by an embedded OPA engine. A policy is compiled once per source and the compiled query is kept by the hash of the
// source, so a hot-deployed version is compiled when it is first used and the old one is dropped when the cache starts over. Evaluations run
// concurrently.
type OPA struct {
	capabilities *ast.Capabilities

	mu       sync.Mutex
	prepared map[[sha256.Size]byte]rego.PreparedEvalQuery
}

func NewOPA() *OPA {
	caps := ast.CapabilitiesForThisVersion()
	allowed := caps.Builtins[:0:0]
	for _, b := range caps.Builtins {
		if !forbiddenBuiltins[b.Name] {
			allowed = append(allowed, b)
		}
	}
	caps.Builtins = allowed
	caps.AllowNet = []string{}
	return &OPA{capabilities: caps, prepared: make(map[[sha256.Size]byte]rego.PreparedEvalQuery)}
}

// validationInput and validationInputWithOthers are the sample calls Validate makes to prove that the rule's value has the expected shape; their
// values are never interpreted.
var (
	validationInput = Input{Resource: "validate", Type: "validate", Issuer: "validate", Claims: map[string]string{"validate": "validate"}}

	validationInputWithOthers = Input{
		Resource: "validate", Type: "validate", Issuer: "validate", Claims: map[string]string{"validate": "validate"},
		Others: map[string]Other{"validate": {Issuer: "validate", Claims: map[string]string{"validate": "validate"}}},
	}
)

// Validate checks that source compiles, is in package policy and defines the rule decision, and that the rule's value has the right shape for a
// sample input.
func (e *OPA) Validate(source string) error {
	module, err := ast.ParseModuleWithOpts("policy.rego", source, ast.ParserOptions{RegoVersion: ast.RegoV1})
	if err != nil {
		return err
	}
	if module.Package.Path.String() != "data."+policyPackage {
		return fmt.Errorf("the policy must be in package %s", policyPackage)
	}
	defined := false
	for _, r := range module.Rules {
		defined = defined || r.Head.Name.String() == decisionRule
	}
	if !defined {
		return fmt.Errorf("the policy defines no rule %s", decisionRule)
	}
	for _, in := range []Input{validationInput, validationInputWithOthers} {
		if _, err := e.Evaluate(source, in); err != nil {
			return err
		}
	}
	return nil
}

func (e *OPA) Evaluate(source string, in Input) (result Result, err error) {
	defer func() {
		if r := recover(); r != nil {
			result, err = Result{}, fmt.Errorf("the policy panicked: %v", r)
		}
	}()

	pq, err := e.prepare(source)
	if err != nil {
		return Result{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), evalTimeout)
	defer cancel()
	rs, err := pq.Eval(ctx, rego.EvalInput(inputOf(in)))
	if err != nil {
		return Result{}, fmt.Errorf("evaluate the policy: %w", err)
	}
	if len(rs) == 0 || len(rs[0].Expressions) == 0 {
		return Result{Decision: Indeterminate, Reason: reasonNoDecision}, nil
	}
	return resultOf(rs[0].Expressions[0].Value)
}

// prepare returns the compiled query of source, compiling it when it has not been seen.
func (e *OPA) prepare(source string) (rego.PreparedEvalQuery, error) {
	key := sha256.Sum256([]byte(source))
	e.mu.Lock()
	pq, ok := e.prepared[key]
	e.mu.Unlock()
	if ok {
		return pq, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), evalTimeout)
	defer cancel()
	pq, err := rego.New(
		rego.Query(query), rego.Module("policy.rego", source), rego.Capabilities(e.capabilities), rego.SetRegoVersion(ast.RegoV1), rego.Strict(true),
	).PrepareForEval(ctx)
	if err != nil {
		return rego.PreparedEvalQuery{}, fmt.Errorf("compile the policy: %w", err)
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.prepared) >= maxPrepared {
		clear(e.prepared)
	}
	e.prepared[key] = pq
	return pq, nil
}

func inputOf(in Input) map[string]any {
	others := make(map[string]any, len(in.Others))
	for t, o := range in.Others {
		others[t] = map[string]any{"issuer": o.Issuer, "claims": stringMap(o.Claims)}
	}
	roles := make([]any, len(in.UserRoles))
	for i, r := range in.UserRoles {
		roles[i] = r
	}
	return map[string]any{
		"resource": in.Resource, "type": in.Type, "issuer": in.Issuer, "claims": stringMap(in.Claims), "userRoles": roles, "others": others,
	}
}

func stringMap(m map[string]string) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// resultOf reads the value of the decision rule.
func resultOf(value any) (Result, error) {
	obj, ok := value.(map[string]any)
	if !ok {
		return Result{}, errors.New("decision: want an object")
	}
	var r Result
	d, ok := obj["decision"].(string)
	if !ok || !Decision(d).valid() {
		return Result{}, fmt.Errorf("decision: unknown decision %v", obj["decision"])
	}
	r.Decision = Decision(d)
	if reason, present := obj["reason"]; present {
		if r.Reason, ok = reason.(string); !ok {
			return Result{}, errors.New("decision: reason must be a string")
		}
	}
	var err error
	if r.Obligations, err = statements(obj["obligations"]); err != nil {
		return Result{}, fmt.Errorf("decision: obligations: %w", err)
	}
	if r.Advice, err = statements(obj["advice"]); err != nil {
		return Result{}, fmt.Errorf("decision: advice: %w", err)
	}
	return r, nil
}

// statements reads a list of objects with an id and attributes, an object of string values, sorted by ID.
func statements(value any) ([]Statement, error) {
	if value == nil {
		return nil, nil
	}
	list, ok := value.([]any)
	if !ok {
		return nil, errors.New("want a list")
	}
	out := make([]Statement, 0, len(list))
	for _, item := range list {
		obj, ok := item.(map[string]any)
		if !ok {
			return nil, errors.New("want a list of objects")
		}
		id, ok := obj["id"].(string)
		if !ok || id == "" {
			return nil, errors.New("every statement needs an id")
		}
		s := Statement{ID: id}
		if attrs, present := obj["attributes"]; present {
			m, ok := attrs.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("statement %q: attributes must be an object", id)
			}
			for k, v := range m {
				str, ok := v.(string)
				if !ok {
					return nil, fmt.Errorf("statement %q: attribute %q must be a string", id, k)
				}
				s.Attributes = append(s.Attributes, Attribute{ID: k, Value: str})
			}
			sort.Slice(s.Attributes, func(i, j int) bool { return s.Attributes[i].ID < s.Attributes[j].ID })
		}
		out = append(out, s)
	}
	return out, nil
}
