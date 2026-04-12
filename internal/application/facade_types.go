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

type applicationFacades struct {
	chatCommands    *ChatCommands
	runtimeCommands *RuntimeCommands
	runtimeQueries  *RuntimeQueries
	sessionCommands *SessionCommands
	sessionQueries  *SessionQueries
	adminCommands   *AdminCommands
	adminQueries    *AdminQueries
}

func newApplicationFacades(kernel *ApplicationKernel) *applicationFacades {
	return &applicationFacades{
		chatCommands:    &ChatCommands{ApplicationKernel: kernel},
		runtimeCommands: &RuntimeCommands{ApplicationKernel: kernel},
		runtimeQueries:  &RuntimeQueries{ApplicationKernel: kernel},
		sessionCommands: &SessionCommands{ApplicationKernel: kernel},
		sessionQueries:  &SessionQueries{ApplicationKernel: kernel},
		adminCommands:   &AdminCommands{ApplicationKernel: kernel},
		adminQueries:    &AdminQueries{ApplicationKernel: kernel},
	}
}
