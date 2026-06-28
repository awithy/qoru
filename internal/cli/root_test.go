package cli

import "testing"

func TestRootCommandHasConfigFlag(t *testing.T) {
	cmd := NewRootCommand()

	flag := cmd.PersistentFlags().Lookup("config")
	if flag == nil {
		t.Fatal("expected --config persistent flag")
	}
	if flag.Shorthand != "c" {
		t.Fatalf("expected -c shorthand, got %q", flag.Shorthand)
	}
}

func TestRootHelpDoesNotRequireConfig(t *testing.T) {
	cmd := NewRootCommand()
	cmd.SetArgs([]string{"--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected help to execute without config, got %v", err)
	}
}
