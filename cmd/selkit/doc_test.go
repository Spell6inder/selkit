package main

import "testing"

func TestReleaseVersion(t *testing.T) {
	for in, want := range map[string]string{
		"v0.1.0":                               "0.1.0",
		"v1.2.3-rc.1":                          "1.2.3-rc.1",
		"v0.1.1-0.20261109120000-bfe0066a1b2c": "",
		"v0.1.1-0.20261109120000-bfe0066a1b2c+dirty": "",
		"v0.1.0+dirty": "",
		"(devel)":      "",
		"":             "",
	} {
		got := ""
		if m := releaseVersion.FindStringSubmatch(in); m != nil {
			got = m[1]
		}
		if got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}
