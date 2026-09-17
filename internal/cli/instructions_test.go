package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/brettinternet/worklease/internal/instructions"
)

func TestInstructionTopicsTextJSONAndDiscovery(t *testing.T) {
	clearWorkleaseEnvironment(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("WORKLEASE_HOME", filepath.Join(home, "uncreated"))
	for _, topic := range instructions.Topics() {
		t.Run(topic, func(t *testing.T) {
			want, err := instructions.For(topic)
			if err != nil {
				t.Fatal(err)
			}
			for _, jsonMode := range []bool{false, true} {
				args := []string{"worklease", "instructions", topic}
				if jsonMode {
					args = append(args, "--json")
				}
				var out, stderr bytes.Buffer
				if err := Run(context.Background(), args, "dev", "unknown", "unknown", &out, &stderr); err != nil {
					t.Fatal(err)
				}
				if stderr.Len() != 0 {
					t.Fatal(stderr.String())
				}
				if jsonMode {
					var got struct {
						OK           bool     `json:"ok"`
						Operation    string   `json:"operation"`
						Topic        string   `json:"topic"`
						Instructions []string `json:"instructions"`
					}
					if err := json.Unmarshal(out.Bytes(), &got); err != nil {
						t.Fatal(err)
					}
					if !got.OK || got.Operation != "instructions" || got.Topic != topic || !reflect.DeepEqual(got.Instructions, want) || strings.Count(out.String(), "\n") != 1 {
						t.Fatalf("unexpected envelope: %s", out.String())
					}
				} else if out.String() != strings.Join(want, "\n")+"\n" {
					t.Fatalf("unexpected text: %s", out.String())
				}
			}
		})
	}
	var directory, stderr bytes.Buffer
	if err := Run(context.Background(), []string{"worklease", "instructions"}, "dev", "unknown", "unknown", &directory, &stderr); err != nil {
		t.Fatal(err)
	}
	for _, topic := range instructions.Topics() {
		if !strings.Contains(directory.String(), topic) {
			t.Fatalf("missing %s in directory: %s", topic, directory.String())
		}
	}
	if strings.Contains(directory.String(), "1.") {
		t.Fatalf("directory dumped guidance: %s", directory.String())
	}
	for _, args := range [][]string{
		{"worklease", "--json", "instructions", "unknown"},
		{"worklease", "--json", "instructions", "setup", "extra"},
	} {
		var out bytes.Buffer
		if err := Run(context.Background(), args, "dev", "unknown", "unknown", &out, &stderr); err == nil {
			t.Fatalf("accepted invalid command %v", args)
		}
		if !json.Valid(out.Bytes()) || strings.Count(out.String(), "\n") != 1 {
			t.Fatalf("invalid error envelope: %s", out.String())
		}
	}
	if entries, err := os.ReadDir(home); err != nil || len(entries) != 0 {
		t.Fatalf("instructions created state: %v, %v", entries, err)
	}
}
