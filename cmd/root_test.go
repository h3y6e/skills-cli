package cmd_test

import (
	"testing"

	"github.com/h3y6e/skolar/cmd"
)

func TestNewRootCmdRegistersCoreCommands(t *testing.T) {
	root := cmd.NewRootCmd("test")

	if got, want := root.Use, "skolar"; got != want {
		t.Fatalf("Use = %q, want %q", got, want)
	}

	if !root.SilenceUsage {
		t.Fatal("SilenceUsage = false, want true")
	}

	checks := []struct {
		name  string
		alias string
	}{
		{name: "add"},
		{name: "check"},
		{name: "update"},
		{name: "list", alias: "ls"},
		{name: "remove", alias: "rm"},
	}

	for _, check := range checks {
		sub, _, err := root.Find([]string{check.name})
		if err != nil {
			t.Fatalf("Find(%q) error = %v", check.name, err)
		}
		if sub == nil || sub.Name() != check.name {
			t.Fatalf("Find(%q) returned %v", check.name, sub)
		}

		if check.alias == "" {
			continue
		}

		aliasSub, _, err := root.Find([]string{check.alias})
		if err != nil {
			t.Fatalf("Find(%q) error = %v", check.alias, err)
		}
		if aliasSub == nil || aliasSub.Name() != check.name {
			t.Fatalf("Find(%q) returned %v, want command %q", check.alias, aliasSub, check.name)
		}
	}
}
