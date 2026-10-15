package sync

import (
	"os"
	"path/filepath"
	"testing"
)

// `--local` has to be able to tell "the account holds none of these" apart from
// "nothing has been synced yet". Both used to come out as "No results found.", which
// answers a question about the account in a situation where the account was never
// read — and now that the first run no longer syncs, having synced nothing is the
// normal starting state rather than an unusual one.
func TestNothingSyncedFor(t *testing.T) {
	home := t.TempDir()
	t.Setenv("__AWLESS_HOME", home)

	const profile, region = "admin", "eu-west-1"

	if !NothingSyncedFor(profile, region) {
		t.Fatal("a home with no synced files reported that something was synced")
	}

	// A region file is enough.
	writeGraph(t, home, profile, region, "infra")
	if NothingSyncedFor(profile, region) {
		t.Error("a synced region file was not noticed")
	}

	// So is a global one on its own, which is where access, dns and cdn land.
	home2 := t.TempDir()
	t.Setenv("__AWLESS_HOME", home2)
	writeGraph(t, home2, profile, "global", "access")
	if NothingSyncedFor(profile, region) {
		t.Error("a synced global file was not noticed")
	}

	// Another profile's data says nothing about this one.
	home3 := t.TempDir()
	t.Setenv("__AWLESS_HOME", home3)
	writeGraph(t, home3, "someone-else", region, "infra")
	if !NothingSyncedFor(profile, region) {
		t.Error("another profile's synced files were counted as this profile's")
	}

	// Nor does another region's, since the local graph is per-region.
	home4 := t.TempDir()
	t.Setenv("__AWLESS_HOME", home4)
	writeGraph(t, home4, profile, "us-east-1", "infra")
	if !NothingSyncedFor(profile, region) {
		t.Error("another region's synced files were counted as this region's")
	}
}

func writeGraph(t *testing.T, home, profile, regionDir, service string) {
	t.Helper()
	dir := filepath.Join(home, "aws", "rdf", profile, regionDir)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, service+fileExt)
	if err := os.WriteFile(path, []byte("<a> <b> <c> .\n"), 0600); err != nil {
		t.Fatal(err)
	}
}

// A profile synced in one region has global files everywhere, so NothingSyncedFor is no
// answer to "was this region's infra synced"; ServiceSyncedFor is.
func TestServiceSyncedFor(t *testing.T) {
	home := t.TempDir()
	t.Setenv("__AWLESS_HOME", home)
	const profile = "admin"

	writeGraph(t, home, profile, "global", "access")
	writeGraph(t, home, profile, "us-west-2", "infra")

	for _, c := range []struct {
		service, region string
		want            bool
	}{
		{"infra", "us-west-2", true},
		{"infra", "eu-west-1", false},
		{"storage", "us-west-2", false},
		{"access", "eu-west-1", true}, // global, whatever the region
		{"dns", "us-west-2", false},
	} {
		if got := ServiceSyncedFor(c.service, profile, c.region); got != c.want {
			t.Errorf("ServiceSyncedFor(%s, %s) = %v, want %v", c.service, c.region, got, c.want)
		}
	}
	if NothingSyncedFor(profile, "eu-west-1") {
		t.Error("precondition: the global file counts as something synced")
	}
}
