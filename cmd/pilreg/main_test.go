package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestHelpOutputGroups(t *testing.T) {
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"--help"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("help command failed: %v", err)
	}
	out := buf.String()
	checks := []string{
		"Registry config options:",
		"Storage config options:",
		"Analysis config options:",
		"Connection options:",
	}
	for _, s := range checks {
		if !strings.Contains(out, s) {
			t.Errorf("expected help to contain %q", s)
		}
	}
}
