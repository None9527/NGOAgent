package application

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
