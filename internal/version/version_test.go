package version

import "testing"

func TestString(t *testing.T) {
	if String() != Version {
		t.Fatal(String())
	}
	Commit = "abc"
	t.Cleanup(func() { Commit = "" })
	if String() != Version+" (abc)" {
		t.Fatal(String())
	}
}
