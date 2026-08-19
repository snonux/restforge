package cli

// globalFlags holds the parsed values of the root command's persistent
// flags, shared with every subcommand. It is a value type constructed by
// [newRoot] and referenced by the closures that implement each command, so
// the flags live in one place and no command reaches at os.Getenv or
// pflag itself -- the same single-source discipline internal/config's
// ConfigEnvVar keeps.
type globalFlags struct {
	// configPath, when non-empty, overrides internal/config's default
	// path resolution. Wired through config.ConfigEnvVar so the override
	// uses the same seam the env var does, rather than a parallel
	// config-loading path (see newRoot's PersistentPreRunE).
	configPath string

	// backendName selects which configured backend a subcommand targets.
	// It is a NAME from config, never a secret -- see ../../AGENTS.md.
	backendName string

	// output is the rendering format one-shot commands render in: "text"
	// (the default) or "json". Kept as a string rather than an enum because
	// it is only ever compared against those two literals, and a string
	// keeps the flag's --help self-describing without a String method.
	output string
}

// validOutputs is the closed set of values --output accepts.
var validOutputs = map[string]bool{
	"text": true,
	"json": true,
}
