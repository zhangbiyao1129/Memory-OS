package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"

	"memory-os/internal/adapter"
)

func main() {
	output, err := run(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Print(output)
}

func run(args []string) (string, error) {
	flags := flag.NewFlagSet("memory-adapter", flag.ContinueOnError)
	adapterName := flags.String("adapter", "transcript", "adapter name")
	inputPath := flags.String("input", "", "input fixture path")
	dryRun := flags.Bool("dry-run", false, "print TurnEvent JSON")
	if err := flags.Parse(args); err != nil {
		return "", err
	}
	if !*dryRun {
		return "", errors.New("only dry-run is supported in this phase")
	}
	if *inputPath == "" {
		return "", errors.New("input is required")
	}
	content, err := os.ReadFile(*inputPath)
	if err != nil {
		return "", err
	}
	selected, err := buildAdapter(*adapterName)
	if err != nil {
		return "", err
	}
	events, err := selected.Convert(adapter.BatchInput{SourceName: *inputPath, Content: content})
	if err != nil {
		return "", err
	}
	encoded, err := json.Marshal(events)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func buildAdapter(name string) (adapter.BatchAdapter, error) {
	config := adapter.Config{UserID: "user_1", OrgID: "org_1", ProjectID: "project_1", AgentID: name}
	switch name {
	case "codex":
		return adapter.NewCodexAdapter(config), nil
	case "generic-mcp":
		return adapter.NewGenericMCPAdapter(config), nil
	case "claude-code":
		return adapter.NewClaudeCodeAdapter(config), nil
	case "opencode":
		return adapter.NewOpenCodeAdapter(config), nil
	case "hermes":
		return adapter.NewHermesAdapter(config), nil
	case "transcript":
		return adapter.NewTranscriptImporter(config), nil
	default:
		return nil, errors.New("unsupported adapter")
	}
}
