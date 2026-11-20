package collector

import "testing"

func TestOSReleaseName(t *testing.T) {
	if got := parseOSRelease("NAME=Ubuntu\nPRETTY_NAME=\"Ubuntu 24.04.3 LTS\"\n"); got != "Ubuntu 24.04.3 LTS" {
		t.Fatalf("unexpected Ubuntu name %q", got)
	}
	if got := parseOSRelease("NAME='Debian GNU/Linux'\nVERSION=13\n"); got != "Debian GNU/Linux 13" {
		t.Fatalf("unexpected Debian name %q", got)
	}
}
