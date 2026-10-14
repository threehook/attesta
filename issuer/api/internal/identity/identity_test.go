package identity

import "testing"

// The DID of the default development seed, as the policies name it.
func TestTheDevelopmentSeedGivesTheKnownDID(t *testing.T) {
	const want = "did:key:z6MkoaRYujmrv9XRRN1maDNHYHEBWDgV4uEtgH5kN4WsBQZA"
	if got := FromSeed("dev-only-insecure-issuer-seed").DID; got != want {
		t.Fatalf("DID %s, want %s", got, want)
	}
}

func TestTheSameSeedGivesTheSameIssuerAndAnotherSeedAnotherOne(t *testing.T) {
	a, b, c := FromSeed("one"), FromSeed("one"), FromSeed("two")
	if a.DID != b.DID || !a.Key.Equal(b.Key) {
		t.Fatal("the same seed must give the same identity")
	}
	if a.DID == c.DID {
		t.Fatal("another seed must give another identity")
	}
}
