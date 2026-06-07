package plumber

type Runtime struct {
	CommandRunner CommandRunner
}

func (r Runtime) inherit(parent Runtime) Runtime {
	if r.CommandRunner == nil {
		r.CommandRunner = parent.CommandRunner
	}

	return r
}
