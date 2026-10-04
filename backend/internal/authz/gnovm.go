// Package authz evaluates authorization policies written in Gno, executed in-process by gnovm — no gno.land chain or node is involved.
package authz

import (
	"fmt"
	"io"
	"sync"

	"github.com/gnolang/gno/gnovm/pkg/gnoenv"
	gno "github.com/gnolang/gno/gnovm/pkg/gnolang"
	"github.com/gnolang/gno/gnovm/pkg/test"
)

// Input is what gets passed into a policy's Authorize function. Type and Issuer are the credential type and issuer that a verified presentation
// established — the policy decides whether that combination is acceptable for Resource.
type Input struct {
	Resource string
	Type     string
	Issuer   string
}

// Result is a policy's allow/deny decision and the human-readable reason for it.
type Result struct {
	Allow  bool
	Reason string
}

// Evaluator runs Gno policy source against an Input and returns a Result.
type Evaluator interface {
	// Validate reports whether source compiles as a Gno package, without running any particular function in it. Used to reject bad policy source
	// before it's stored.
	Validate(source string) error
	// Evaluate loads source as a Gno package and calls its exported Authorize(resource, credType, issuer string) (bool, string) function.
	Evaluate(source string, in Input) (Result, error)
}

// GnoVM is an Evaluator backed by an embedded gnovm interpreter. The underlying gno.Store (which indexes gno.land's standard library and examples on
// disk, see gnoenv.RootDir) is expensive to build, so it's built once as a base store and reused; each call forks a TransactionStore from it
// (store.go's BeginTransaction), runs a fresh gno.Machine against that fork, and never calls Write() on it. This isolates every evaluation's
// throwaway package (gno's own store.SetCachePackage panics if the same pkgPath is cached twice on one store) and discards it afterwards instead of
// leaking into the shared base store's caches. Calls are serialized by mu — the base store's thread safety for concurrent transaction forks is not
// documented upstream, so this trades throughput for correctness until confirmed.
type GnoVM struct {
	mu     sync.Mutex
	base   gno.Store
	output io.Writer
}

func NewGnoVM(output io.Writer) (*GnoVM, error) {
	rootDir := gnoenv.RootDir()
	_, base := test.ProdStore(rootDir, output, nil)
	return &GnoVM{base: base, output: output}, nil
}

// policyPkgPath is reused for every call: each call runs against its own transaction-store fork (see withMachine), so there's no cross-call collision
// despite the shared path.
const policyPkgPath = "attesta/policy"

// validationInput is the sample call Validate makes to prove the policy's Authorize has the expected signature; its values are never interpreted.
var validationInput = Input{Resource: "validate", Type: "validate", Issuer: "validate"}

// Validate checks that source compiles and that its Authorize function exists with the signature Evaluate calls, by running it once against
// validationInput. A policy that panics on that call is rejected too.
func (e *GnoVM) Validate(source string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	_, err := e.evaluate(source, validationInput)
	return err
}

func (e *GnoVM) Evaluate(source string, in Input) (Result, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	return e.evaluate(source, in)
}

// evaluate runs source's Authorize against in. Callers must hold e.mu.
func (e *GnoVM) evaluate(source string, in Input) (result Result, err error) {
	runErr := e.withMachine(func(m *gno.Machine) error {
		file, err := parseFile(m, source)
		if err != nil {
			return err
		}
		m.RunFiles(file)

		expr := fmt.Sprintf("Authorize(%q, %q, %q)", in.Resource, in.Type, in.Issuer)
		ex, err := m.ParseExpr(expr)
		if err != nil {
			return fmt.Errorf("parse call expression: %w", err)
		}
		m.MaybeInjectCurForEval(ex)

		results := m.Eval(ex)
		if len(results) != 2 {
			return fmt.Errorf("Authorize: expected 2 results, got %d", len(results))
		}
		if results[0].T == nil || results[0].T.Kind() != gno.BoolKind || results[1].T == nil || results[1].T.Kind() != gno.StringKind {
			return fmt.Errorf("Authorize: want results (bool, string)")
		}
		result = Result{Allow: results[0].GetBool(), Reason: results[1].GetString()}
		return nil
	})
	return result, runErr
}

// withMachine forks a throwaway TransactionStore off the shared base store, builds a Machine against that fork loaded as package "policy" at
// policyPkgPath, and hands it to fn. The fork is never Write()-written back, so it and everything cached on it (the package, its block nodes) are
// simply dropped when withMachine returns — the base store stays clean across calls. gnovm reports compile/runtime errors by panicking, so this also
// converts those panics to plain errors for callers — no panic should ever cross this package's boundary.
func (e *GnoVM) withMachine(fn func(m *gno.Machine) (err error)) (err error) {
	store := e.base.BeginTransaction(nil, nil, nil, nil)

	m := gno.NewMachineWithOptions(gno.MachineOptions{
		Output:        e.output,
		Store:         store,
		MaxAllocBytes: 50_000_000,
		Context:       test.Context("", policyPkgPath, nil),
	})
	defer m.Release()

	pn := gno.NewPackageNode(gno.Name("policy"), policyPkgPath, &gno.FileSet{})
	pv := pn.NewPackage(m.Alloc)
	m.Store.SetBlockNode(pn)
	m.Store.SetCachePackage(pv)
	m.SetActivePackage(pv)

	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("gno policy error: %v", r)
		}
	}()
	return fn(m)
}

func parseFile(m *gno.Machine, source string) (*gno.FileNode, error) {
	file, err := m.ParseFile("policy.gno", source)
	if err != nil {
		return nil, fmt.Errorf("parse policy source: %w", err)
	}
	return file, nil
}
