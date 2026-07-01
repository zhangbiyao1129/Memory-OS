package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"

	"memory-os/internal/importer"
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
	flags := flag.NewFlagSet("memory-importer", flag.ContinueOnError)
	source := flags.String("source", "mem0", "source type")
	batch := flags.String("batch", "", "batch id")
	input := flags.String("input", "", "input path")
	dryRun := flags.Bool("dry-run", false, "preview import")
	apply := flags.Bool("apply", false, "apply import")
	exportBundle := flags.Bool("export-bundle", false, "export markdown/rag bundle after apply")
	if err := flags.Parse(args); err != nil {
		return "", err
	}
	if *batch == "" || *input == "" {
		return "", errors.New("batch and input are required")
	}
	content, err := os.ReadFile(*input)
	if err != nil {
		return "", err
	}
	service := importer.NewService(importer.NewMemoryRepository())
	request := importer.ImportRequest{BatchID: *batch, SourceType: importer.SourceType(*source), Content: content, Scope: importer.DefaultScope()}
	if *dryRun {
		result, err := service.DryRun(request)
		if err != nil {
			return "", err
		}
		return encode(result)
	}
	if *apply {
		result, err := service.Apply(request)
		if err != nil {
			return "", err
		}
		if *exportBundle {
			bundle, err := service.ExportBundle(*batch)
			if err != nil {
				return "", err
			}
			return bundle.Markdown + "\nmetadata:\n" + bundle.MetadataJSON + "\nsource_refs:\n" + bundle.SourceRefsJSON, nil
		}
		return encode(result)
	}
	return "", errors.New("either dry-run or apply is required")
}

func encode(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}
