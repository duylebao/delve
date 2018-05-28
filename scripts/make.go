package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

const DelveMainPackagePath = "github.com/derekparker/delve/cmd/dlv"

var Backend string
var Verbose, AllBackends bool

func NewMakeCommands() *cobra.Command {
	RootCommand := &cobra.Command{
		Use:   "make.go",
		Short: "make script for delve.",
	}

	RootCommand.AddCommand(&cobra.Command{
		Use:   "check-cert",
		Short: "Check certificate for macOS.",
		Run:   checkCertCmd,
	})

	RootCommand.AddCommand(&cobra.Command{
		Use:   "build",
		Short: "Build delve",
		Run:   buildCmd,
	})

	RootCommand.AddCommand(&cobra.Command{
		Use:   "install",
		Short: "Installs delve",
		Run:   installCmd,
	})

	test := &cobra.Command{
		Use:   "test-full",
		Short: "Tests delve",
		Run:   testFullCmd,
	}
	test.PersistentFlags().BoolVarP(&Verbose, "verbose", "v", false, "verbose tests")
	test.PersistentFlags().BoolVarP(&AllBackends, "all", "a", true, "tests all backends")
	RootCommand.AddCommand(test)

	testBackend := &cobra.Command{
		Use:   "test-backend <backend>",
		Short: "Test one backend",
		Run:   testBackendCmd,
	}
	RootCommand.AddCommand(testBackend)

	testProc := &cobra.Command{
		Use:   "test-proc-run <test pattern>",
		Short: "Runs proc test",
		Run:   testProcCmd,
	}
	testProc.PersistentFlags().BoolVarP(&Verbose, "verbose", "v", false, "verbose tests")
	testProc.PersistentFlags().StringVar(&Backend, "backend", "native", "Selects backend")
	RootCommand.AddCommand(testProc)

	testIntegration := &cobra.Command{
		Use:   "test-integration-run <test pattern>",
		Short: "Runs integration test",
		Run:   testIntegrationCmd,
	}
	testIntegration.PersistentFlags().BoolVarP(&Verbose, "verbose", "v", false, "verbose tests")
	testIntegration.PersistentFlags().StringVar(&Backend, "backend", "native", "Selects backend")
	RootCommand.AddCommand(testIntegration)

	RootCommand.AddCommand(&cobra.Command{
		Use:   "vendor",
		Short: "vendors dependencies",
		Run:   vendorCmd,
	})

	return RootCommand
}

func checkCertCmd(cmd *cobra.Command, args []string) {
	// If we're on OSX make sure the proper CERT env var is set.
	if os.Getenv("TRAVIS") == "true" || runtime.GOOS != "darwin" || os.Getenv("CERT") != "" {
		return
	}

	x := exec.Command("scripts/gencert.sh")
	err := x.Run()
	if x.ProcessState != nil && !x.ProcessState.Success() {
		fmt.Printf("An error occurred when generating and installing a new certificate\n")
		os.Exit(1)
	}
	if err != nil {
		log.Fatal(err)
	}
	os.Setenv("CERT", "dlv-cert")
}

func strflatten(v []interface{}) []string {
	r := []string{}
	for _, s := range v {
		switch s := s.(type) {
		case []string:
			r = append(r, s...)
		case string:
			r = append(r, s)
		}
	}
	return r
}

func executeq(cmd string, args ...interface{}) {
	x := exec.Command(cmd, strflatten(args)...)
	x.Stdout = os.Stdout
	x.Stderr = os.Stderr
	x.Env = os.Environ()
	err := x.Run()
	if x.ProcessState != nil && !x.ProcessState.Success() {
		os.Exit(1)
	}
	if err != nil {
		log.Fatal(err)
	}
}

func execute(cmd string, args ...interface{}) {
	fmt.Printf("%s %s\n", cmd, strings.Join(strflatten(args), " "))
	executeq(cmd, args...)
}

func getoutput(cmd string, args ...interface{}) string {
	x := exec.Command(cmd, strflatten(args)...)
	x.Env = os.Environ()
	out, err := x.CombinedOutput()
	if err != nil {
		log.Fatal(err)
	}
	if !x.ProcessState.Success() {
		os.Exit(1)
	}
	return string(out)
}

func codesign(path string) {
	execute("codesign", "-s", os.Getenv("CERT"), path)
}

func buildCmd(cmd *cobra.Command, args []string) {
	checkCertCmd(nil, nil)
	execute("go", "build", buildFlags(), DelveMainPackagePath)
	if runtime.GOOS == "darwin" && os.Getenv("CERT") != "" {
		codesign("./dlv")
	}
}

func installCmd(cmd *cobra.Command, args []string) {
	checkCertCmd(nil, nil)
	execute("go", "install", buildFlags(), DelveMainPackagePath)
	if runtime.GOOS != "darwin" {
		codesign(installedExecutablePath())
	}
}

func installedExecutablePath() string {
	if gobin := os.Getenv("GOBIN"); gobin != "" {
		return filepath.Join(gobin, "dlv")
	}
	gopath := strings.Split(getoutput("go", "env", "GOPATH"), ":")
	return gopath[0]
}

func buildFlags() []string {
	buildSHA, err := exec.Command("git", "rev-parse", "HEAD").CombinedOutput()
	if err != nil {
		log.Fatal(err)
	}
	ldFlags := "-X main.Build=" + strings.TrimSpace(string(buildSHA))
	if runtime.GOOS == "darwin" {
		ldFlags = "-s " + ldFlags
	}
	return []string{fmt.Sprintf("-ldflags=%q", ldFlags)}
}

func testFlags() []string {
	wd, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
	testFlags := []string{"-count", "1", "-p", "1"}
	if Verbose {
		testFlags = append(testFlags, "-v")
	}
	if runtime.GOOS == "darwin" {
		testFlags = append(testFlags, "-exec="+wd+"/scripts/testsign")
	}
	return testFlags
}

func testFullCmd(cmd *cobra.Command, args []string) {
	checkCertCmd(nil, nil)

	if os.Getenv("TRAVIS") == "true" && runtime.GOOS == "darwin" {
		os.Setenv("PROCTEST", "lldb")
		executeq("sudo", "-E", "go", "test", testFlags(), allPackages())
		return
	}

	defaultBackend := "native"
	if runtime.GOOS == "darwin" {
		defaultBackend = "lldb"
	}
	os.Setenv("PROCTEST", defaultBackend)

	executeq("go", "test", testFlags(), buildFlags(), allPackages())

	if !AllBackends {
		return
	}

	if inpath("lldb-server") {
		fmt.Println("\nTesting LLDB backend")
		testBackend("lldb")
	}

	if inpath("rr") {
		fmt.Println("\nTesting RR backend")
		testBackend("rr")
	}
}

func inpath(exe string) bool {
	path, _ := exec.LookPath(exe)
	return path != ""
}

func allPackages() []string {
	r := []string{}
	for _, dir := range strings.Split(getoutput("go", "list", "./..."), "\n") {
		dir = strings.TrimSpace(dir)
		if dir == "" || strings.Contains(dir, "/vendor/") || strings.Contains(dir, "/scripts") {
			continue
		}
		r = append(r, dir)
	}
	sort.Strings(r)
	return r
}

func testBackend(backend string) {
	execute("go", "test", testFlags(), buildFlags(), "github.com/derekparker/delve/pkg/proc", "github.com/derekparker/delve/service/test", "github.com/derekparker/delve/pkg/terminal", "-backend="+backend)
}

func testBackendCmd(cmd *cobra.Command, args []string) {
	checkCertCmd(nil, nil)
	if len(args) != 1 {
		fmt.Fprintf(os.Stderr, "wrong number of arguments\n")
		os.Exit(1)
	}
	testBackend(args[0])
}

func testProcCmd(cmd *cobra.Command, args []string) {
	execute("go", "test", testFlags(), buildFlags(), "-test.v", "-test.run="+args[0], "-backend="+Backend, "github.com/derekparker/delve/pkg/proc")
}

func testIntegrationCmd(cmd *cobra.Command, args []string) {
	execute("go", "test", testFlags(), buildFlags(), "-test.v", "-test.run="+args[0], "-backend="+Backend, "github.com/derekparker/delve/service/test")
}

func vendorCmd(cmd *cobra.Command, args []string) {
	execute("glide", "up", "-v")
	execute("glide-vc", "--use-lock-file", "--no-tests", "--only-code")
}

func main() {
	NewMakeCommands().Execute()
}
