package main

import "testing"

func TestListenAddrOK(t *testing.T) {
	for _, a := range []string{"127.0.0.1:5300", "localhost:1", "[::1]:1"} {
		if !ListenAddrOK(a) {
			t.Errorf("%s should be allowed", a)
		}
	}
	for _, a := range []string{"0.0.0.0:5300", ":5300", "192.168.1.5:80", "garbage"} {
		if ListenAddrOK(a) {
			t.Errorf("%s should be rejected", a)
		}
	}
}
