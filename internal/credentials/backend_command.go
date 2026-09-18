package credentials

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

type commandSpec struct {
	Name  string
	Args  []string
	Stdin string
}

type commandBackend struct {
	probe string
	put   func(id string, secret []byte) commandSpec
	get   func(id string) commandSpec
	del   func(id string) commandSpec
	look  func(string) (string, error)
	run   func(name string, args []string, stdin string) (string, error)
}

func (b commandBackend) Available() error {
	look := b.look
	if look == nil {
		look = exec.LookPath
	}
	if _, err := look(b.probe); err != nil {
		return ErrSecureStoreUnavailable
	}
	return nil
}

func (b commandBackend) exec(spec commandSpec) (string, error) {
	run := b.run
	if run == nil {
		run = runCommand
	}
	return run(spec.Name, spec.Args, spec.Stdin)
}

func (b commandBackend) Put(id string, secret []byte) error {
	if err := b.Available(); err != nil {
		return err
	}
	if _, err := b.exec(b.put(id, secret)); err != nil {
		return ErrSecureStoreUnavailable
	}
	return nil
}

func (b commandBackend) Get(id string) ([]byte, error) {
	if err := b.Available(); err != nil {
		return nil, err
	}
	out, err := b.exec(b.get(id))
	if err != nil {
		return nil, ErrCredentialUnavailable
	}
	out = strings.TrimRight(out, "\r\n")
	if out == "" {
		return nil, ErrCredentialUnavailable
	}
	return []byte(out), nil
}

func (b commandBackend) Delete(id string) error {
	if err := b.Available(); err != nil {
		return err
	}
	_, _ = b.exec(b.del(id))
	return nil
}

func runCommand(name string, args []string, stdin string) (string, error) {
	cmd := exec.Command(name, args...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("secure store command failed")
	}
	return stdout.String(), nil
}

func darwinBackend() commandBackend {
	return commandBackend{
		probe: "security",
		put: func(id string, secret []byte) commandSpec {
			return commandSpec{Name: "security", Args: []string{
				"add-generic-password", "-a", "benes", "-s", credService(id), "-w", string(secret), "-U",
			}}
		},
		get: func(id string) commandSpec {
			return commandSpec{Name: "security", Args: []string{
				"find-generic-password", "-a", "benes", "-s", credService(id), "-w",
			}}
		},
		del: func(id string) commandSpec {
			return commandSpec{Name: "security", Args: []string{
				"delete-generic-password", "-a", "benes", "-s", credService(id),
			}}
		},
	}
}

func linuxBackend() commandBackend {
	return commandBackend{
		probe: "secret-tool",
		put: func(id string, secret []byte) commandSpec {
			return commandSpec{
				Name:  "secret-tool",
				Args:  []string{"store", "--label", "Benes", "service", "benes", "id", id},
				Stdin: string(secret),
			}
		},
		get: func(id string) commandSpec {
			return commandSpec{Name: "secret-tool", Args: []string{"lookup", "service", "benes", "id", id}}
		},
		del: func(id string) commandSpec {
			return commandSpec{Name: "secret-tool", Args: []string{"clear", "service", "benes", "id", id}}
		},
	}
}

func credService(id string) string { return "Benes/" + id }
