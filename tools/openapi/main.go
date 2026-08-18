package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/KubeOrch/core/tools/openapi/internal/openapicheck"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printUsage(stderr)
		return 2
	}

	switch args[0] {
	case "validate":
		if len(args) != 2 {
			printUsage(stderr)
			return 2
		}
		return validateFile(args[1], stdout, stderr)
	case "breaking":
		if len(args) != 3 {
			printUsage(stderr)
			return 2
		}
		return compareFiles(args[1], args[2], stdout, stderr)
	case "breaking-ref":
		if len(args) != 4 {
			printUsage(stderr)
			return 2
		}
		return compareGitRef(args[1], args[2], args[3], stdout, stderr)
	default:
		writef(stderr, "unknown command %q\n", args[0])
		printUsage(stderr)
		return 2
	}
}

func validateFile(path string, stdout, stderr io.Writer) int {
	data, err := os.ReadFile(path)
	if err != nil {
		writef(stderr, "read %s: %v\n", path, err)
		return 2
	}
	violations, err := openapicheck.Validate(data, path)
	if err != nil {
		writeln(stderr, err)
		return 2
	}
	if len(violations) > 0 {
		writef(stderr, "%s violates KubeOrch API conventions:\n", path)
		for _, violation := range violations {
			writef(stderr, "- %s\n", violation)
		}
		return 1
	}

	writef(stdout, "%s is a valid KubeOrch OpenAPI contract\n", path)
	return 0
}

func compareFiles(basePath, revisionPath string, stdout, stderr io.Writer) int {
	base, err := os.ReadFile(basePath)
	if err != nil {
		writef(stderr, "read base contract %s: %v\n", basePath, err)
		return 2
	}
	revision, err := os.ReadFile(revisionPath)
	if err != nil {
		writef(stderr, "read revised contract %s: %v\n", revisionPath, err)
		return 2
	}
	return reportCompatibility(base, basePath, revision, revisionPath, stdout, stderr)
}

func compareGitRef(baseRef, gitSpecPath, revisionPath string, stdout, stderr io.Writer) int {
	if strings.ContainsAny(baseRef, "\r\n") || strings.ContainsAny(gitSpecPath, "\r\n") || strings.ContainsAny(revisionPath, "\r\n") {
		writeln(stderr, "git reference and spec paths must be single-line values")
		return 2
	}
	command := exec.Command("git", "show", baseRef+":"+gitSpecPath)
	base, err := command.Output()
	if err != nil {
		writef(stderr, "read %s:%s: %v\n", baseRef, gitSpecPath, err)
		return 2
	}
	revision, err := os.ReadFile(revisionPath)
	if err != nil {
		writef(stderr, "read revised contract %s: %v\n", revisionPath, err)
		return 2
	}
	return reportCompatibility(base, baseRef+":"+gitSpecPath, revision, revisionPath, stdout, stderr)
}

func reportCompatibility(base []byte, baseSource string, revision []byte, revisionSource string, stdout, stderr io.Writer) int {
	changes, err := openapicheck.Compare(base, baseSource, revision, revisionSource)
	if err != nil {
		writeln(stderr, err)
		return 2
	}
	if len(changes) == 0 {
		writef(stdout, "%s is backward compatible with %s\n", revisionSource, baseSource)
		return 0
	}

	writef(stderr, "%s has %d breaking change(s) from %s:\n", revisionSource, len(changes), baseSource)
	for _, change := range changes {
		writef(stderr, "- [%s %s] %s %s: %s\n", change.Level, change.ID, change.Method, change.Path, change.Description)
	}
	return 1
}

func printUsage(writer io.Writer) {
	writeln(writer, "usage:")
	writeln(writer, "  openapi-check validate <spec>")
	writeln(writer, "  openapi-check breaking <base-spec> <revised-spec>")
	writeln(writer, "  openapi-check breaking-ref <base-git-ref> <git-spec-path> <revised-spec>")
}

func writef(writer io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(writer, format, args...)
}

func writeln(writer io.Writer, args ...any) {
	_, _ = fmt.Fprintln(writer, args...)
}
