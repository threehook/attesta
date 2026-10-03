package authz

import (
	"io"
	"testing"
)

// Evaluate serializes every call behind a single mutex (see GnoVM's doc comment). This measures what that actually costs per call, rather than
// leaving it as an unmeasured assumption.
func BenchmarkGnoVMEvaluate(b *testing.B) {
	vm, err := NewGnoVM(io.Discard)
	if err != nil {
		b.Fatalf("NewGnoVM: %v", err)
	}
	in := Input{Resource: "diploma-vault", Issuer: "trusted-university"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := vm.Evaluate(diplomaPolicyV1, in); err != nil {
			b.Fatalf("Evaluate: %v", err)
		}
	}
}
