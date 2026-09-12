package main

import (
	"fmt"
	"reflect"
	"testing"
)

func TestReleaseTargetsCoverSupportedNativeArchives(t *testing.T) {
	var names []string
	for _, target := range targets {
		names = append(names, fmt.Sprintf("worklease-v1.2.3-%s-%s.tar.gz", target.platform, target.assetArch))
	}
	want := []string{
		"worklease-v1.2.3-linux-x64.tar.gz",
		"worklease-v1.2.3-linux-arm64.tar.gz",
		"worklease-v1.2.3-macos-x64.tar.gz",
		"worklease-v1.2.3-macos-arm64.tar.gz",
	}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("archive names=%v want=%v", names, want)
	}
}
