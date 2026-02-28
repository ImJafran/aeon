package tools

// DNATools holds references to built-in tools that need security wiring.
type DNATools struct {
	ShellExec *ShellExecTool
	FileRead  *FileReadTool
	FileWrite *FileWriteTool
	FileEdit  *FileEditTool
	WebRead   *WebReadTool
}

// RegisterDNATools registers all built-in (DNA) tools into the registry.
// Returns a DNATools struct so security can be wired to all tools that need it.
func RegisterDNATools(r *Registry) *DNATools {
	shellExec := NewShellExec()
	fileRead := NewFileRead()
	fileWrite := NewFileWrite()
	fileEdit := NewFileEdit()
	webRead := NewWebRead()

	r.Register(shellExec)
	r.Register(fileRead)
	r.Register(fileWrite)
	r.Register(fileEdit)
	r.Register(webRead)

	return &DNATools{
		ShellExec: shellExec,
		FileRead:  fileRead,
		FileWrite: fileWrite,
		FileEdit:  fileEdit,
		WebRead:   webRead,
	}
}
