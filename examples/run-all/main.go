// Runs the examples in sequence and prints ✓/✗ per script.
// It is the suite against the real backend: needs INFI_SECRET_KEY.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"time"
)

func main() {
	if os.Getenv("INFI_SECRET_KEY") == "" {
		fmt.Fprintln(os.Stderr, "set INFI_SECRET_KEY to an sk_test_ key from your own sandbox account")
		os.Exit(1)
	}
	_, self, _, _ := runtime.Caller(0)
	examples := filepath.Dir(filepath.Dir(self)) // .../examples
	root := filepath.Dir(examples)

	entries, err := os.ReadDir(examples)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	numbered := regexp.MustCompile(`^\d\d-`)
	var names []string
	for _, e := range entries {
		if e.IsDir() && numbered.MatchString(e.Name()) {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	type result struct {
		name    string
		ok      bool
		seconds int
	}
	var results []result
	for _, name := range names {
		fmt.Printf("\n════════ %s ════════\n", name)
		started := time.Now()
		cmd := exec.Command("go", "run", "./examples/"+name)
		cmd.Dir = root
		cmd.Stdout, cmd.Stderr, cmd.Env = os.Stdout, os.Stderr, os.Environ()
		runErr := cmd.Run()
		results = append(results, result{name, runErr == nil, int(time.Since(started).Seconds() + 0.5)})
	}

	fmt.Println("\n════════ Summary ════════")
	failed := 0
	for _, r := range results {
		mark := "✓"
		if !r.ok {
			mark = "✗"
			failed++
		}
		fmt.Printf("%s %s (%ds)\n", mark, r.name, r.seconds)
	}
	fmt.Printf("\n%d/%d scripts ok\n", len(results)-failed, len(results))
	if failed > 0 {
		os.Exit(1)
	}
}
