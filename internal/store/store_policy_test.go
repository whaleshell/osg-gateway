package store_test

import (
	"path/filepath"
	"testing"

	"github.com/zorneth/osg-gateway/internal/store"
)

func TestSetBasePolicyTracksRevisions(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(dir, "gw-test")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertSandbox(store.Sandbox{Name: "demo"}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetBasePolicy("demo", "version: 1\n"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetBasePolicy("demo", "version: 1\nnetwork:\n  default: deny\n"); err != nil {
		t.Fatal(err)
	}
	revs, err := st.ListPolicyRevisions("demo")
	if err != nil {
		t.Fatal(err)
	}
	if len(revs) != 2 || revs[1].Rev != 2 || revs[1].Status != store.PolicyStatusLoaded {
		t.Fatalf("%+v", revs)
	}
	r, err := st.GetPolicyRevision("demo", 1)
	if err != nil || r.YAML != "version: 1\n" {
		t.Fatalf("%+v %v", r, err)
	}
	// Cap: fill beyond max
	for i := 0; i < store.MaxPolicyRevisions+5; i++ {
		if err := st.SetBasePolicy("demo", "v: "+filepath.Base(dir)+"\n"); err != nil {
			t.Fatal(err)
		}
	}
	revs, _ = st.ListPolicyRevisions("demo")
	if len(revs) > store.MaxPolicyRevisions {
		t.Fatalf("cap exceeded: %d", len(revs))
	}
}
