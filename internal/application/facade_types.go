package application

// Command/query wrappers embed the shared application kernel and form the
// internal facade layer between the dependency graph and exported services.
type ChatCommands struct {
	*ApplicationKernel
}

type RuntimeCommands struct {
	*ApplicationKernel
}

type RuntimeQueries struct {
	*ApplicationKernel
}

type SessionCommands struct {
	*ApplicationKernel
}

type SessionQueries struct {
	*ApplicationKernel
}

type AdminCommands struct {
	*ApplicationKernel
}

type AdminQueries struct {
	*ApplicationKernel
}
