package mcp

import (
	"reflect"
	"testing"
)

func TestEnvironmentWithOverrides(t *testing.T) {
	got := environmentWithOverrides(
		[]string{"PATH=/usr/bin", "TOKEN=old", "HOME=/tmp"},
		map[string]string{"TOKEN": "new", "API_KEY": "secret"},
	)
	want := []string{"PATH=/usr/bin", "HOME=/tmp", "API_KEY=secret", "TOKEN=new"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("environmentWithOverrides() = %#v, want %#v", got, want)
	}
}

func TestEnvironmentWithOverridesUsesProcessEnvironmentByDefault(t *testing.T) {
	if got := environmentWithOverrides([]string{"PATH=/usr/bin"}, nil); got != nil {
		t.Fatalf("environmentWithOverrides() = %#v, want nil", got)
	}
}
