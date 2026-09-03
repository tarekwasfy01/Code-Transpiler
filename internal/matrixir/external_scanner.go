package matrixir

// ExternalScannerAdapter is the language-neutral representation of the
// Tree-sitter external-scanner ABI. Implementations are supplied by a
// temporary, locally compiled scanner host; the parser stores Serialized state
// per GLR version and never treats ExternalLexState as scanner state.
type ExternalScannerAdapter interface {
	Create() (payload uintptr, err error)
	Destroy(payload uintptr) error
	Scan(payload uintptr, input *ExternalScanInput) (ExternalScanResult, error)
	Serialize(payload uintptr) ([]byte, error)
	Deserialize(payload uintptr, state []byte) error
}

type ExternalScanInput struct {
	Source       string
	Offset       int
	ValidSymbols []bool
}

type ExternalScanResult struct {
	// Accepted distinguishes a valid external token with symbol id 0 from
	// scanner refusal. Tree-sitter external token index 0 is a real token.
	Accepted       bool
	AcceptedSymbol int
	EndOffset      int
	Serialized     []byte
}

type externalScannerRuntime interface {
	Scan(source string, offset int, valid []bool) (ExternalScanResult, error)
	Restore(state []byte)
	Clone(state []byte) (externalScannerRuntime, error)
	Close()
}
