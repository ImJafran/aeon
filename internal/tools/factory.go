package tools

// RegisterDNATools registers all built-in (DNA) tools into the registry.
// Returns the shell_exec tool so security can be wired in.
func RegisterDNATools(r *Registry) *ShellExecTool {
	shellExec := NewShellExec()
	r.Register(shellExec)
	r.Register(NewFileRead())
	r.Register(NewFileWrite())
	r.Register(NewFileEdit())
	r.Register(NewWebRead())
	return shellExec
}
