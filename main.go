package main

import (
	"flag"
	"fmt"
	"os"
	"path"
	"runtime"
)

var (
	// File list that will be created on current run
	HANDLED_FILES = make(StringSet)
	// Keeper ignores top level directories of file system
	IGNORED_DIRS = make(StringSet)

	BASEDIR       string
	KEEPER_SYSDIR string
	// File list that has been created on previous run
	PREVIOUS_LIST string

	DRYRUN        bool
	VERBOSE       bool
	CONCURRENCY   int = 1
	FORWARD_AGENT bool

	VERSION = "2.0"
)

func usage() {
	s := fmt.Sprintf("Usage:\n  %s [options] command [args]\n\n", path.Base(os.Args[0]))
	s += "Commands:\n"
	s += "  init\n"
	s += "      initialize an existing repo\n\n"
	s += "  sync | check-files [--dryrun]\n"
	s += "      sync repository files to the file system\n\n"
	s += "  remote-sync [-n] [-A] [--dryrun] REPODIR [HOSTS]\n"
	s += "      run 'git pull' on all remote agents or given hosts\n\n"
	s += "  remote-run [-n] [-A] COMMAND [HOSTS]\n"
	s += "      run 'command' on all remote agents or given hosts\n\n"
	s += "  test-template FILENAME\n"
	s += "      test an existing template file\n\n"
	s += "  version\n"
	s += "      print version\n\n"
	s += "Options:\n"
	s += "  -dryrun\n"
	s += "      perform a simulation of events that would occur but actually do nothing\n"
	s += "  -n INT\n"
	s += "      concurrent ssh sessions (default 1)\n"
	s += "  -A\n"
	s += "      enable forwarding of the authentication agent connection\n"
	s += "  -verbose\n"
	s += "      enable verbose output\n\n"

	fmt.Fprintf(os.Stderr, s)

	os.Exit(2)
}

func init() {
	b, err := os.Getwd()
	if err != nil {
		fatal(err)
	}
	BASEDIR = path.Join(b, "base")
	KEEPER_SYSDIR = path.Join(b, ".keeper")
	PREVIOUS_LIST = path.Join(KEEPER_SYSDIR, ".previous_list")

	IGNORED_DIRS.Add(
		"/base",
		"/bin",
		"/boot",
		"/dev",
		"/etc",
		"/home",
		"/lib",
		"/proc",
		"/root",
		"/sbin",
		"/sys",
		"/usr",
		"/var",
	)
}

func main() {
	flag.BoolVar(&VERBOSE, "verbose", VERBOSE, "")

	flag.Usage = usage
	flag.Parse()

	if flag.NArg() == 0 {
		flag.Usage()
	}

	cmdInit := flag.NewFlagSet("", flag.ExitOnError)
	cmdInit.Usage = usage

	cmdSync := flag.NewFlagSet("", flag.ExitOnError)
	cmdSync.Usage = usage
	cmdSync.BoolVar(&DRYRUN, "dryrun", DRYRUN, "")

	cmdRSync := flag.NewFlagSet("", flag.ExitOnError)
	cmdRSync.Usage = usage
	cmdRSync.BoolVar(&DRYRUN, "dryrun", DRYRUN, "")
	cmdRSync.IntVar(&CONCURRENCY, "n", CONCURRENCY, "")
	cmdRSync.BoolVar(&FORWARD_AGENT, "A", FORWARD_AGENT, "")

	cmdRRun := flag.NewFlagSet("", flag.ExitOnError)
	cmdRRun.Usage = usage
	cmdRRun.IntVar(&CONCURRENCY, "n", CONCURRENCY, "")
	cmdRRun.BoolVar(&FORWARD_AGENT, "A", FORWARD_AGENT, "")

	cmdTpl := flag.NewFlagSet("", flag.ExitOnError)
	cmdTpl.Usage = usage

	cmdVer := flag.NewFlagSet("", flag.ExitOnError)
	cmdVer.Usage = usage

	if os.Getenv("DRYRUN") != "" {
		DRYRUN = true
	}

	switch _, err := os.Stat(".dryrun"); {
	case err == nil:
		DRYRUN = true
	case !os.IsNotExist(err):
		fatal(err)
	}

	command := flag.Arg(0)

	switch command {
	case "init":
		cmdInit.Parse(flag.Args()[1:])
		if err := initRepo(); err != nil {
			fatal("repository initialization error:", err)
		}
	case "sync", "check-files":
		if err := initVariables(); err != nil {
			fatal("init variables error:", err)
		}
		cmdSync.Parse(flag.Args()[1:])
		switch _, err := os.Stat(KEEPER_SYSDIR); {
		case os.IsNotExist(err):
			fmt.Println("Run  'keeper init'  first to initialize Keeper")
			os.Exit(3)
		case err != nil:
			fatal(err)
		}
		if err := syncRepo(); err != nil {
			fatal("syncing error:", err)
		}
	case "remote-sync", "rs":
		cmdRSync.Parse(flag.Args()[1:])
		var hosts []string
		switch {
		case cmdRSync.NArg() < 1:
			flag.Usage()
		case cmdRSync.NArg() > 1:
			hosts = cmdRSync.Args()[1:]
		}

		cmd := "keeper sync"
		if DRYRUN {
			cmd = "DRYRUN=1 " + cmd
		}
		cmd = "MANUAL=1 git pull; " + cmd
		cmd = "cd " + cmdRSync.Arg(0) + "; " + cmd

		if err := remoteCommand(cmd, hosts); err != nil {
			fatal("remote syncing error:", err)
		}
	case "remote-run", "rr":
		cmdRRun.Parse(flag.Args()[1:])
		var hosts []string
		switch {
		case cmdRRun.NArg() < 1:
			flag.Usage()
		case cmdRRun.NArg() > 1:
			hosts = cmdRRun.Args()[1:]
		}
		if err := remoteCommand(cmdRRun.Arg(0), hosts); err != nil {
			fatal("remote execution error:", err)
		}
	case "test-template", "tt":
		if err := initVariables(); err != nil {
			fatal("init variables error:", err)
		}
		cmdTpl.Parse(flag.Args()[1:])
		if cmdTpl.NArg() != 1 {
			flag.Usage()
		}
		if err := testTemplate(cmdTpl.Arg(0)); err != nil {
			fatal("template execution error:", err)
		}
	case "version", "ver", "v":
		fmt.Printf("v%s, (built %s)\n", VERSION, runtime.Version())
	default:
		flag.Usage()
	}
}
