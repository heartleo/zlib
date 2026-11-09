package cli

// Most tests exercise the cover pipeline itself, so covers start enabled.
// runCoverProgram resets this for real runs; TestCoversHiddenWithoutProtocol
// covers the disabled state.
func init() { coverGraphicsAvailable.Store(true) }
