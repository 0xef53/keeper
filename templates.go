package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"text/template"
)

var (
	funcMap = template.FuncMap{
		"byIfname": getIfaceByName,
		"ifelse":   ternariusIf,
	}

	ENVS = new(Variables)
)

type CustomVariables map[string]interface{}

type Variables struct {
	Hostname string
	Network  []NetIf
	X        CustomVariables
}

// Making the Variables structure.
func initVariables() error {
	switch h, err := os.Hostname(); {
	case err == nil:
		ENVS.Hostname = h
	case os.IsNotExist(err):
		ENVS.Hostname = "(none)"
	default:
		return err
	}

	switch ifs, err := getNetIfaces(); {
	case err == nil:
		ENVS.Network = ifs
	default:
		return err
	}

	switch out, err := exec.Command("./myenvs").Output(); {
	case err == nil:
		if err := json.Unmarshal(out, &ENVS.X); err != nil {
			return err
		}
	case !os.IsNotExist(err):
		return fmt.Errorf("%s: %s", err, out)
	}

	return nil
}

// Executes a given template tplname. On success writes results to dstname
// if it's defined or writes to the standard output otherwise.
func executeTemplate(tplname, dstname string, mode os.FileMode, uid, gid int) error {
	T, err := template.New("main").Option("missingkey=error").Funcs(funcMap).ParseFiles(tplname)
	if err != nil {
		return err
	}

	var dstfile *os.File
	if dstname != "" {
		tmpfile, err := ioutil.TempFile(filepath.Dir(dstname), "keeper")
		if err != nil {
			return err
		}
		defer tmpfile.Close()
		defer os.Remove(tmpfile.Name())

		dstfile = tmpfile
	} else {
		dstfile = os.Stdout
	}

	if err := T.ExecuteTemplate(dstfile, path.Base(tplname), ENVS); err != nil {
		return err
	}
	if err := dstfile.Close(); err != nil {
		return err
	}

	if dstname != "" {
		if err := os.Chmod(dstfile.Name(), mode); err != nil {
			return err
		}
		if err := os.Chown(dstfile.Name(), uid, gid); err != nil {
			return err
		}
		return os.Rename(dstfile.Name(), dstname)
	}

	return nil
}

// Type NetIf represents network interface's parameters.
type NetIf struct {
	Index    int
	Name     string
	Hwaddr   string
	IP4Addrs []string
}

// Returns a list of the system's network interfaces and their parameters.
func getNetIfaces() ([]NetIf, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}

	iflist := make([]NetIf, 0, len(ifaces))
	for _, iface := range ifaces {
		addrs, err := iface.Addrs()
		if err != nil {
			return nil, err
		}

		ip4addrs := []string{}
		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok && ipnet.IP.To4() != nil {
				ip4addrs = append(ip4addrs, ipnet.IP.String())
			}
		}

		iflist = append(iflist, NetIf{
			iface.Index,
			iface.Name,
			iface.HardwareAddr.String(),
			ip4addrs,
		})
	}

	return iflist, nil
}

// Returns an interface named ifname or returns an empty structure.
func getIfaceByName(ifaces []NetIf, ifname string) NetIf {
	for _, iface := range ifaces {
		if iface.Name == ifname {
			return iface
		}
	}
	return NetIf{}
}

func ternariusIf(flag bool, retValues string) string {
	var trueValue, falseValue string

	switch fields := strings.Split(retValues, "|"); {
	case len(fields) == 1:
		trueValue = fields[0]
		falseValue = trueValue
	case len(fields) >= 2:
		trueValue = fields[0]
		falseValue = fields[1]
	}

	if flag {
		return trueValue
	}
	return falseValue
}
