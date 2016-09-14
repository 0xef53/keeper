package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/user"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"gopkg.in/yaml.v2"

	"github.com/0xef53/go-group"
)

// Type describes parameters of repository file.
type RepositoryFile struct {
	Path       string      `yaml:"-"`
	FSPath     string      `yaml:"-"`
	Owner      string      `yaml:"owner"`
	Group      string      `yaml:"group"`
	Uid        int         `yaml:"-"`
	Gid        int         `yaml:"-"`
	Perms      os.FileMode `yaml:"perms"`
	Mode       os.FileMode `yaml:"-"`
	IsTemplate bool        `yaml:"-"`
}

func NewRepositoryFile(repopath string) (*RepositoryFile, error) {
	f := RepositoryFile{
		Path:  repopath,
		Owner: "root",
		Group: "root",
	}

	f.FSPath = filepath.Join("/", strings.TrimPrefix(repopath, BASEDIR))
	if path.Ext(f.Path) == ".template" {
		f.IsTemplate = true
		f.FSPath = strings.TrimSuffix(f.FSPath, ".template")
	}

	fi, err := os.Lstat(f.Path)
	if err != nil {
		return nil, err
	}
	f.Mode = fi.Mode()

	// If repopath is a file, then applying .#_globparams first and then .#FILENAME_params.
	// If repopath is a directory, then applying only .#_params from this directory.
	var paramsFile string
	switch {
	case fi.IsDir():
		paramsFile = path.Join(f.Path, ".#_params")
	default:
		paramsFile = path.Join(path.Dir(f.Path), fmt.Sprintf(".#%s_params", path.Base(f.FSPath)))
	}

	// if it's a file
	if !fi.IsDir() {
		globParamsFile := path.Join(path.Dir(f.Path), ".#_globparams")
		if c, err := ioutil.ReadFile(globParamsFile); err == nil {
			if err := yaml.Unmarshal(c, &f); err != nil {
				return nil, fmt.Errorf("Params error: %s", err)
			}
			if f.Perms != 0 && f.Mode&os.ModeSymlink == 0 {
				f.Mode = (f.Mode &^ os.ModePerm) ^ permsToFileMode(f.Perms)
			}
		}
	}
	// If it's a directory
	if c, err := ioutil.ReadFile(paramsFile); err == nil {
		if err := yaml.Unmarshal(c, &f); err != nil {
			return nil, fmt.Errorf("Params error: %s", err)
		}
		if f.Perms != 0 && f.Mode&os.ModeSymlink == 0 {
			f.Mode = (f.Mode &^ os.ModePerm) ^ permsToFileMode(f.Perms)
		}
	}

	// Looking for UID/GID
	switch u, err := user.Lookup(f.Owner); {
	case err == nil:
		uid, err := strconv.Atoi(u.Uid)
		if err != nil {
			return nil, err
		}
		f.Uid = uid
	default:
		f.Owner = "root"
	}
	switch g, err := group.Lookup(f.Group); {
	case err == nil:
		gid, err := strconv.Atoi(g.Gid)
		if err != nil {
			return nil, err
		}
		f.Gid = gid
	default:
		f.Group = "root"
	}

	return &f, nil
}

// Checks whether the file from repository is the same as file in the file system.
func (rf *RepositoryFile) Exists() bool {
	// Always overwrite template files
	if rf.IsTemplate {
		return false
	}

	fsfileInfo, err := os.Lstat(rf.FSPath)
	if err != nil {
		return false
	}

	// Checking attributes and owner/group IDs
	if fsfileInfo.Mode() != rf.Mode {
		return false
	}
	if fsfileInfo.Sys() == nil {
		return false
	}
	fsfileUid := int(fsfileInfo.Sys().(*syscall.Stat_t).Uid)
	fsfileGid := int(fsfileInfo.Sys().(*syscall.Stat_t).Gid)

	if fsfileUid != rf.Uid || fsfileGid != rf.Gid {
		return false
	}

	switch {
	case rf.Mode.IsDir():
		if !fsfileInfo.Mode().IsDir() {
			return false
		}
	case rf.Mode&os.ModeSymlink != 0:
		if fsfileInfo.Mode()&os.ModeSymlink == 0 {
			return false
		}
		dest1, err := os.Readlink(rf.Path)
		if err != nil {
			return false
		}
		dest2, err := os.Readlink(rf.FSPath)
		if err != nil {
			return false
		}
		if dest1 != dest2 {
			return false
		}
	case rf.Mode.IsRegular():
		if !fsfileInfo.Mode().IsRegular() {
			return false
		}
		if !equalContent(rf.Path, rf.FSPath) {
			return false
		}
	}
	return true
}

// Syncs the file/directory from repository to the file system
// and sets the access attributes and the owner/group.
func (rf *RepositoryFile) Sync() error {
	switch {
	case rf.Mode.IsDir():
		switch dfi, err := os.Stat(rf.FSPath); {
		case err == nil:
			if !(dfi.Mode().IsDir()) {
				return fmt.Errorf("non directory destination already exists: %s (%q)", rf.FSPath, dfi.Mode().String())
			}
		case !os.IsNotExist(err):
			return err
		}

		if err := os.MkdirAll(rf.FSPath, 0755); err != nil {
			return err
		}
		if err := os.Chmod(rf.FSPath, rf.Mode); err != nil {
			return err
		}
		if err := os.Chown(rf.FSPath, rf.Uid, rf.Gid); err != nil {
			return err
		}
	case rf.Mode&os.ModeSymlink != 0:
		switch dfi, err := os.Lstat(rf.FSPath); {
		case err == nil:
			if dfi.Mode()&os.ModeSymlink == 0 {
				return fmt.Errorf("non symbolic link destination file already exists: %s", rf.FSPath)
			}
			if err := os.Remove(rf.FSPath); err != nil {
				return err
			}
		case !os.IsNotExist(err):
			return err
		}

		dest, err := os.Readlink(rf.Path)
		if err != nil {
			return err
		}
		if err := os.Symlink(dest, rf.FSPath); err != nil {
			return err
		}
	case rf.Mode.IsRegular():
		switch dfi, err := os.Stat(rf.FSPath); {
		case err == nil:
			if !(dfi.Mode().IsRegular()) {
				return fmt.Errorf("non regular destination file already exists: %s (%q)", rf.FSPath, dfi.Mode().String())
			}
		case !os.IsNotExist(err):
			return err
		}

		if rf.IsTemplate {
			if err := executeTemplate(rf.Path, rf.FSPath, rf.Mode, rf.Uid, rf.Gid); err != nil {
				return err
			}
		} else {
			if err := copyFileContents(rf.Path, rf.FSPath, rf.Mode, rf.Uid, rf.Gid); err != nil {
				return err
			}
		}
	}

	return nil
}

func (rf RepositoryFile) String() string {
	tplMark := "-"
	if rf.IsTemplate {
		tplMark = "t"
	}
	return fmt.Sprintf(" %s %s %s:%s base%s  ->  %s", tplMark, rf.Mode, rf.Owner, rf.Group, rf.FSPath, rf.FSPath)
}
