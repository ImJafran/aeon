package tools

// RegisterDNATools registers all built-in (DNA) tools into the registry.
func RegisterDNATools(r *Registry) {
	r.Register(NewShellExec())
	r.Register(NewFileRead())
	r.Register(NewFileWrite())
	r.Register(NewFileEdit())
	r.Register(NewWebRead())
}
