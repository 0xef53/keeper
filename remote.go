package main

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"

	"github.com/0xef53/go-sshwrapper"
)

var outLock sync.RWMutex

type RemoteAgent struct {
	User string
	Host string
	Port int
}

func ParseRemoteAgents(entries []string) ([]RemoteAgent, error) {
	if entries == nil {
		switch out, err := exec.Command("./agents").Output(); {
		case err == nil:
			entries = strings.Fields(string(out))
		case !os.IsNotExist(err):
			return nil, fmt.Errorf("%s: %s", err, out)
		}
	}

	parse := func(s string) (*RemoteAgent, error) {
		a := RemoteAgent{User: "root", Port: 22}

		switch fields := strings.Split(s, "@"); {
		case len(fields) == 1:
		case len(fields) == 2:
			a.User, s = fields[0], fields[1]
		default:
			return nil, fmt.Errorf("incorrect format: %s", s)
		}

		switch fields := strings.Split(s, ":"); {
		case len(fields) == 1:
			a.Host = fields[0]
		case len(fields) == 2:
			a.Host = fields[0]
			d, err := strconv.Atoi(fields[1])
			if err != nil {
				return nil, err
			}
			a.Port = d
		default:
			return nil, fmt.Errorf("incorrect format: %s", s)
		}

		return &a, nil
	}

	agents := make([]RemoteAgent, 0, len(entries))

	for _, s := range entries {
		a, err := parse(s)
		if err != nil {
			return nil, err
		}
		agents = append(agents, *a)
	}

	return agents, nil
}

func execRemoteCmd(agents []RemoteAgent, cmd string, concurrency int, forwardAgent bool) (errors []error) {
	authSock := os.Getenv("SSH_AUTH_SOCK")

	limit := make(chan struct{}, concurrency)

	execute := func(user, host string, port int) error {
		conn, err := sshwrapper.NewSSHConn(user, host, port, authSock, forwardAgent)
		if err != nil {
			return err
		}

		var output *os.File

		switch {
		case concurrency > 1:
			tmpfile, err := ioutil.TempFile("", ".keeper_report_")
			if err != nil {
				return err
			}
			_ = os.Remove(tmpfile.Name())
			defer tmpfile.Close()

			output = tmpfile
		default:
			output = os.Stdout
		}

		if concurrency == 1 {
			fmt.Printf("--> %s@%s:%d\n", user, host, port)
		}

		if err := conn.Run(cmd, nil, output, output); err != nil {
			return err
		}

		if concurrency > 1 {
			if err := output.Sync(); err != nil {
				return err
			}
			if _, err := output.Seek(int64(os.SEEK_SET), 0); err != nil {
				return err
			}

			outLock.Lock()
			defer outLock.Unlock()

			fmt.Printf("--> %s@%s:%d\n", user, host, port)
			io.Copy(os.Stdout, output)
		}

		fmt.Println()

		return nil
	}

	var wg sync.WaitGroup

	for _, agent := range agents {
		limit <- struct{}{}
		wg.Add(1)

		go func(a RemoteAgent) {
			defer wg.Done()
			defer func() { <-limit }()

			if err := execute(a.User, a.Host, a.Port); err != nil {
				errors = append(errors, fmt.Errorf("%s: %s", a.Host, err))
			}
		}(agent)
	}

	wg.Wait()

	return errors
}
