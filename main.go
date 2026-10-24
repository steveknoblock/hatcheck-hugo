// Command hatcheck-hugo turns a Hatcheck export archive into a directory of
// Markdown files laid out for Hugo (or any static site generator that reads
// a plain directory of .md files).
//
// It reads only the .tar.gz archive that Hatcheck's export produces
// (`hatcheck export`, or GET /export). It never talks to a running Hatcheck
// server and never reads its on-disk store, so it makes no API calls and has
// no credentials.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run is main with its inputs and outputs passed in, so it can be tested.
// It returns the process exit code.
func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("hatcheck-hugo", flag.ContinueOnError)
	fs.SetOutput(stderr)
	namespace := fs.String("namespace", "", "Namespace to render (default: the archive's own namespace)")
	outDir := fs.String("o", "", "Output directory (default: ./<namespace>)")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: hatcheck-hugo [-namespace <ns>] [-o <dir>] <archive.tar.gz>")
		fmt.Fprintln(stderr)
		fmt.Fprintln(stderr, "Renders every name in a namespace of a Hatcheck export as Markdown:")
		fmt.Fprintln(stderr, "a leaf becomes <slug>.md, a Collection becomes <slug>/ with an _index.md.")
		fmt.Fprintln(stderr, "Flags must come before the archive path.")
		fmt.Fprintln(stderr)
		fmt.Fprintln(stderr, "Examples:")
		fmt.Fprintln(stderr, "  hatcheck export -source bob -namespace bob")
		fmt.Fprintln(stderr, "  hatcheck-hugo bob.tar.gz")
		fmt.Fprintln(stderr, "  hatcheck-hugo -o ./content bob.tar.gz")
		fmt.Fprintln(stderr, "  hatcheck-hugo -namespace bob everything.tar.gz")
		fmt.Fprintln(stderr)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return 2
	}

	a, err := ReadArchiveFile(fs.Arg(0))
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}

	ns := *namespace
	if ns == "" {
		ns = a.Manifest.Namespace
	}
	if ns == "" {
		fmt.Fprintln(stderr, "error: this archive is not scoped to a namespace; pass -namespace")
		return 1
	}

	dest := *outDir
	if dest == "" {
		dest = "./" + ns
	}

	if err := Render(a, ns, dest); err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}

	fmt.Fprintf(stdout, "rendered namespace %q to %s\n", ns, dest)
	return 0
}
