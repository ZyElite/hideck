package driver

import "fmt"

type NetTools struct {
	defaultInterface string
}

// NewNetTools accepts the original no-argument form and the interim bound-
// interface form.
func NewNetTools(defaultInterface ...string) *NetTools {
	if len(defaultInterface) > 1 {
		panic("NewNetTools: expected zero or one interface name")
	}
	tools := &NetTools{}
	if len(defaultInterface) == 1 {
		tools.defaultInterface = defaultInterface[0]
	}
	return tools
}

type NetToolError struct {
	Op   string
	Args string
	Err  error
}

func (e *NetToolError) Error() string {
	if e.Args == "" {
		return fmt.Sprintf("%s 失败: %v", e.Op, e.Err)
	}
	return fmt.Sprintf("%s %s 失败: %v", e.Op, e.Args, e.Err)
}

func (e *NetToolError) Unwrap() error { return e.Err }

func wrapErr(operation, arguments string, err error) error {
	if err == nil {
		return nil
	}
	return &NetToolError{Op: operation, Args: arguments, Err: err}
}

func (n *NetTools) interfaceName(explicit []string) (string, error) {
	if len(explicit) > 1 {
		return "", fmt.Errorf("expected at most one interface name, got %d", len(explicit))
	}
	if len(explicit) == 1 {
		return explicit[0], nil
	}
	if n != nil && n.defaultInterface != "" {
		return n.defaultInterface, nil
	}
	return "", fmt.Errorf("interface name is required")
}
