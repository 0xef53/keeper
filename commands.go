package main

import (
	"bufio"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// Creates Keeper's system directory (KEEPER_SYSDIR) at the root of git-repository
// and adds post-merge hook.
func initRepo() error {
	switch _, err := os.Stat(".git"); {
	case os.IsNotExist(err):
		return fmt.Errorf(".git directory not found")
	case err != nil:
		return err
	}

	// System dir
	if err := os.Mkdir(KEEPER_SYSDIR, 0750); err != nil && !os.IsExist(err) {
		return err
	}

	// Submodules init & update
	if out, err := exec.Command("git", "submodule", "init").CombinedOutput(); err != nil {
		return fmt.Errorf("%s: %s", err, out)
	}
	if out, err := exec.Command("git", "submodule", "update").CombinedOutput(); err != nil {
		return fmt.Errorf("%s: %s", err, out)
	}

	// Git hook
	b := []byte(`#!/bin/sh -e
git submodule init
git submodule update

[ -n "${MANUAL:-}" ] || {
    echo
    keeper check-files
}
`)

	githook := ".git/hooks/post-merge"
	tmpfile := githook + ".NEW"
	if err := ioutil.WriteFile(tmpfile, b, 0755); err != nil {
		return err
	}

	return os.Rename(tmpfile, githook)
}

// Syncs each file/directory from BASEDIR directory to the file system.
// Cleans removed files/directories at the end.
func syncRepo() error {
	if DRYRUN {
		fmt.Fprintln(os.Stderr, "( !!! running with option DRYRUN, nothing to do !!! )")
	}

	fmt.Println("--> Updating configuration files:")

	for p := range walk(BASEDIR) {
		if err := syncFile(p); err != nil {
			warn(err)
		}
	}

	fmt.Println()
	fmt.Println("--> Removing deleted files:")

	if err := removeDeleted(); err != nil {
		return fmt.Errorf("removing deleted files: %s", err)
	}

	if !DRYRUN {
		tmpfile, err := ioutil.TempFile(filepath.Dir(PREVIOUS_LIST), "keeper")
		if err != nil {
			return err
		}
		defer func() {
			tmpfile.Close()
			os.Remove(tmpfile.Name())
		}()

		w := bufio.NewWriter(tmpfile)
		for k := range HANDLED_FILES {
			fmt.Fprintln(w, k)
		}
		w.Flush()

		if err := os.Rename(tmpfile.Name(), PREVIOUS_LIST); err != nil {
			return err
		}
	}

	return nil
}

func syncFile(p string) error {
	repofile, err := NewRepositoryFile(p)
	if err != nil {
		return err
	}

	if IGNORED_DIRS.Has(repofile.FSPath) {
		return nil
	}

	HANDLED_FILES.Add(repofile.FSPath)

	if repofile.Exists() {
		if VERBOSE {
			fmt.Println(repofile)
		}
		return nil
	}

	if DRYRUN {
		fmt.Println(repofile)
		return nil
	}

	switch err := repofile.Sync(); {
	case err == nil:
		fmt.Println(repofile)
	default:
		return err
	}

	return nil
}

// Removes files/directories that had been deleted from git repository.
func removeDeleted() error {
	f, err := os.Open(PREVIOUS_LIST)
	switch {
	case os.IsNotExist(err):
		return nil
	case err != nil:
		return err
	}
	defer f.Close()

	prevHandled := make(StringSet)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) == "" {
			continue
		}
		prevHandled.Add(scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("reading %s: %s", PREVIOUS_LIST, err)
	}

	diff := make(StringSet)
	for file := range prevHandled {
		if !HANDLED_FILES.Has(file) {
			diff.Add(file)
		}
	}

	// Removing files
	for file := range diff {
		switch fi, err := os.Lstat(file); {
		case err == nil:
			if fi.Mode().IsDir() {
				continue
			} else {
				diff.Remove(file)
			}
		case os.IsNotExist(err):
			diff.Remove(file)
			continue
		default:
			return err
		}

		if DRYRUN {
			fmt.Printf(" -f %s\n", file)
			continue
		}

		switch err := os.Remove(file); {
		case err == nil || os.IsNotExist(err):
			fmt.Printf(" -f %s\n", file)
		default:
			return err
		}
	}

	// Removing directories
	for dir, _ := range diff {
		if DRYRUN {
			fmt.Printf(" -d %s\n", dir)
			continue
		}

		switch err := os.Remove(dir); {
		case err == nil || os.IsNotExist(err):
			fmt.Printf(" -d %s\n", dir)
		default:
			if _err, ok := err.(*os.PathError); ok && _err.Err == syscall.ENOTEMPTY {
				fmt.Printf(" -d %s (directory not empty so not removed)\n", dir)
			} else {
				return err
			}
		}
	}

	return nil
}

func remoteCommand(cmd string, hosts []string) error {
	agents, err := ParseRemoteAgents(hosts)
	if err != nil {
		return fmt.Errorf("agents parsing error: %s", err)
	}

	if CONCURRENCY < 1 {
		CONCURRENCY = 1
	}

	if errors := execRemoteCmd(agents, cmd, CONCURRENCY, FORWARD_AGENT); len(errors) > 0 {
		for _, err := range errors {
			warn(err)
		}
	}

	return nil
}

// Tries to execute a given template file and writes results
// to the standard output on success.
func testTemplate(tplname string) error {
	return executeTemplate(tplname, "", 0, 0, 0)
}
