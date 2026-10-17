package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"
)

// modulePath is where the deck is published. A binary built with `go install`
// knows it from its build info; this is the fallback for a `go build` in the
// repository.
const modulePath = "github.com/unixsuperhero/tuios-deck"

// buildVersion describes the running binary: the module version when it was
// installed with `go install`, else the commit it was built from.
func buildVersion() string {
	if version != "dev" {
		return version
	}
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	if v := bi.Main.Version; v != "" && v != "(devel)" {
		return v
	}
	var rev, when, dirty string
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.time":
			when = s.Value
		case "vcs.modified":
			if s.Value == "true" {
				dirty = " (modified)"
			}
		}
	}
	if rev == "" {
		return "dev"
	}
	if len(rev) > 12 {
		rev = rev[:12]
	}
	return "dev+" + rev + dirty + " " + when
}

// goEnv cleans the environment for the go command: no workspace, no local
// module flags, so the install resolves the published module.
func goEnv() []string {
	env := os.Environ()
	out := env[:0]
	for _, e := range env {
		if strings.HasPrefix(e, "GOWORK=") || strings.HasPrefix(e, "GOFLAGS=") {
			continue
		}
		out = append(out, e)
	}
	return append(out, "GOWORK=off")
}

// latestVersion asks the go command, and through it the module proxy, for
// the newest published version.
func latestVersion() (string, error) {
	cmd := exec.Command("go", "list", "-m", "-json", modulePath+"@latest")
	cmd.Env = goEnv()
	cmd.Dir = os.TempDir() // outside any module, so the query is about the published one
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("go list: %s", strings.TrimSpace(stderr.String()))
	}
	var m struct {
		Version string `json:"Version"`
		Time    string `json:"Time"`
	}
	if err := json.Unmarshal(out, &m); err != nil {
		return "", err
	}
	return m.Version, nil
}

// goBin is where `go install` puts binaries.
func goBin() string {
	for _, key := range []string{"GOBIN", "GOPATH"} {
		out, err := exec.Command("go", "env", key).Output()
		if err != nil {
			continue
		}
		p := strings.TrimSpace(string(out))
		if p == "" {
			continue
		}
		if key == "GOPATH" {
			p = filepath.Join(strings.Split(p, string(os.PathListSeparator))[0], "bin")
		}
		return p
	}
	return ""
}

func runUpdate(args []string) error {
	fs := flag.NewFlagSet("deck update", flag.ExitOnError)
	check := fs.Bool("check", false, "only report the running and the latest version; install nothing")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `deck update [version] [--check]

Installs the latest published deck with "go install %s/cmd/deck@latest",
or the version given (v0.3.0, a commit, a branch). The binary lands in
$GOBIN, else $GOPATH/bin, which is where go install always puts it; a copy
built by hand somewhere else is not touched.

Flags:
`, modulePath)
		fs.PrintDefaults()
	}
	fs.Parse(args)
	if _, err := exec.LookPath("go"); err != nil {
		return fmt.Errorf("update needs the go command on PATH")
	}
	want := "latest"
	if fs.NArg() > 0 {
		want = fs.Arg(0)
	}

	fmt.Printf("running:  %s\n", buildVersion())
	latest, err := latestVersion()
	if err != nil {
		fmt.Printf("latest:   unknown (%v)\n", err)
	} else {
		fmt.Printf("latest:   %s\n", latest)
	}
	if *check {
		if err == nil && latest == buildVersion() {
			fmt.Println("up to date")
		}
		return nil
	}

	target := modulePath + "/cmd/deck@" + want
	fmt.Printf("install:  go install %s\n", target)
	cmd := exec.Command("go", "install", target)
	cmd.Env = goEnv()
	cmd.Dir = os.TempDir()
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("go install failed: %w", err)
	}
	bin := filepath.Join(goBin(), "deck")
	fmt.Printf("installed: %s\n", bin)
	if out, err := exec.Command(bin, "version").Output(); err == nil {
		fmt.Printf("now:      %s", out)
	}
	self, _ := os.Executable()
	if self != "" && bin != "" {
		s, _ := filepath.EvalSymlinks(self)
		b, _ := filepath.EvalSymlinks(bin)
		if s != "" && b != "" && s != b {
			fmt.Printf("note:     this command ran from %s, not the installed copy; make sure %s is first on PATH\n", s, filepath.Dir(bin))
		}
	}
	return nil
}
