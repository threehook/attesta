// Command spike is a minimal, throwaway example of embedding gnovm in-process (no gno.land chain) and calling an exported Gno function from Go. See
// internal/authz for the real, concurrency-safe implementation.
package main

import (
	"fmt"
	"os"

	"github.com/gnolang/gno/gnovm/pkg/gnoenv"
	gno "github.com/gnolang/gno/gnovm/pkg/gnolang"
	"github.com/gnolang/gno/gnovm/pkg/test"
)

const policyFile = "../examples/simple-gui/policies/diploma_check.gno"

func main() {
	if err := run("diploma-vault", "Diploma", "trusted-university"); err != nil {
		fmt.Fprintln(os.Stderr, "allow case failed:", err)
		os.Exit(1)
	}
	if err := run("diploma-vault", "Diploma", "diploma-mill"); err != nil {
		fmt.Fprintln(os.Stderr, "deny case failed:", err)
		os.Exit(1)
	}
}

func run(resource, credType, issuer string) error {
	rootDir := gnoenv.RootDir()
	output := test.OutputWithError(os.Stdout, os.Stderr)
	_, store := test.ProdStore(rootDir, output, nil)

	const pkgPath = "zk-puoi/policy" // not a realm path: evaluated in-memory, no persistence
	ctx := test.Context("", pkgPath, nil)

	m := gno.NewMachineWithOptions(gno.MachineOptions{
		Output:        output,
		Store:         store,
		MaxAllocBytes: 500_000_000,
		Context:       ctx,
	})
	defer m.Release()

	pn := gno.NewPackageNode(gno.Name("policy"), pkgPath, &gno.FileSet{})
	pv := pn.NewPackage(m.Alloc)
	m.Store.SetBlockNode(pn)
	m.Store.SetCachePackage(pv)
	m.SetActivePackage(pv)

	file := m.MustReadFile(policyFile)
	m.RunFiles(file)

	expr := fmt.Sprintf("Authorize(%q, %q, %q)", resource, credType, issuer)
	ex, err := m.ParseExpr(expr)
	if err != nil {
		return fmt.Errorf("parse expr: %w", err)
	}
	m.MaybeInjectCurForEval(ex)
	results := m.Eval(ex)
	if len(results) != 2 {
		return fmt.Errorf("expected 2 results, got %d", len(results))
	}

	allow := results[0].GetBool()
	reason := results[1].GetString()
	fmt.Printf("resource=%q type=%q issuer=%q -> allow=%v reason=%q\n", resource, credType, issuer, allow, reason)
	return nil
}
