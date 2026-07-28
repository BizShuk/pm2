package wizard

// WriteOptions is the shared output contract for interactive, install, and
// direct ecosystem writes.
type WriteOptions struct {
	Output  string
	Format  string
	Force   bool
	NoMerge bool
}

// RunOptions and InstallOptions remain aliases for source compatibility.
type (
	RunOptions     = WriteOptions
	InstallOptions = WriteOptions
)

// DefaultWriteOptions returns the default JS write behavior.
func DefaultWriteOptions() WriteOptions {
	return WriteOptions{Format: defaultFormat}
}
