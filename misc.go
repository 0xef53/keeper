package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"strings"
	"syscall"
)

const (
	StickyBit os.FileMode = 1 << (9 + iota)
	SetgidBit
	SetuidBit
)

// Converts given permissions to a FileMode structure.
func permsToFileMode(perms os.FileMode) (mode os.FileMode) {
	mode = perms &^ 07000

	if perms&StickyBit > 0 {
		mode |= os.ModeSticky
	}
	if perms&SetgidBit > 0 {
		mode |= os.ModeSetgid
	}
	if perms&SetuidBit > 0 {
		mode |= os.ModeSetuid
	}

	return mode
}

// Returns true if both files definitely have the same content.
func equalContent(fname1, fname2 string) bool {
	if fname1 == fname2 {
		return true
	}
	f1, err := os.Open(fname1)
	if err != nil {
		return false
	}
	defer f1.Close()
	f2, err := os.Open(fname2)
	if err != nil {
		return false
	}
	defer f2.Close()

	fi1, err := f1.Stat()
	if err != nil {
		return false
	}
	fi2, err := f2.Stat()
	if err != nil {
		return false
	}
	if fi1.Size() != fi2.Size() {
		return false
	}

	rd1 := bufio.NewReaderSize(f1, 256*1024)
	rd2 := bufio.NewReaderSize(f2, 256*1024)

	buf1 := make([]byte, syscall.Getpagesize())
	buf2 := make([]byte, syscall.Getpagesize())

	var f1EOFseen, f2EOFseen bool

	for {
		n1, err := rd1.Read(buf1)
		switch err {
		case nil:
		case io.EOF:
			f1EOFseen = true
		default:
			return false
		}

		n2, err := rd2.Read(buf2)
		switch err {
		case nil:
		case io.EOF:
			f2EOFseen = true
		default:
			return false
		}
		if !bytes.Equal(buf1[:n1], buf2[:n2]) {
			return false
		}
		if f1EOFseen && f2EOFseen {
			return true
		}
	}
}

// Copies content from srcname to dstname and sets the access attributes and the owner/group.
func copyFileContents(srcname, dstname string, mode os.FileMode, uid, gid int) error {
	in, err := os.Open(srcname)
	if err != nil {
		return err
	}
	defer in.Close()

	tmpfile, err := ioutil.TempFile(filepath.Dir(dstname), "keeper")
	if err != nil {
		return err
	}
	defer tmpfile.Close()
	defer os.Remove(tmpfile.Name())

	if _, err = io.Copy(tmpfile, in); err != nil {
		return err
	}
	if err := tmpfile.Close(); err != nil {
		return err
	}

	if err := os.Chmod(tmpfile.Name(), mode); err != nil {
		return err
	}
	if err := os.Chown(tmpfile.Name(), uid, gid); err != nil {
		return err
	}

	return os.Rename(tmpfile.Name(), dstname)
}

// Type based on map for simple operation with string lists.
type StringSet map[string]struct{}

func (ss StringSet) Add(values ...string) {
	for _, v := range values {
		ss[v] = struct{}{}
	}
}

func (ss StringSet) Has(v string) bool {
	_, ok := ss[v]
	return ok
}

func (ss StringSet) Remove(v string) {
	delete(ss, v)
}

// Walks the BASEDIR and returns all visited files/directories via channel.
func walk(rootdir string) chan string {
	cPaths := make(chan string, 1)

	walkFn := func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if p == rootdir || strings.HasPrefix(path.Base(p), ".#") {
			return nil
		}
		cPaths <- p
		return nil
	}

	go func() {
		defer close(cPaths)
		if err := filepath.Walk(rootdir, walkFn); err != nil {
			fatal("walk error:", err)
		}
	}()

	return cPaths
}

func fatal(v ...interface{}) {
	fmt.Fprintf(os.Stderr, "[Fatal] %s", fmt.Sprintln(v...))
	os.Exit(1)
}

func warn(v ...interface{}) {
	fmt.Fprintf(os.Stderr, "[Warn] %s", fmt.Sprintln(v...))
}
