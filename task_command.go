package plumber

// Creates a NewCommand attached to the current task.
func (t *Task[Pipe]) CreateCommand(command string, args ...string) *Command[Pipe] {
	return NewCommand(t, command, args...)
}

// Returns the attached commands to this task.
func (t *Task[Pipe]) GetCommands() []*Command[Pipe] {
	return t.commands
}

// Returns the attached commands to this task as a slice of jobs.
func (t *Task[Pipe]) GetCommandJobs() []Job {
	j := []Job{}
	for _, c := range t.commands {
		j = append(j, c.Job())
	}

	return j
}

// Returns the attached commands to this task as a job to run as sequence depending on their definition order.
func (t *Task[Pipe]) GetCommandJobAsJobSequence() Job {
	j := t.GetCommandJobs()

	if len(j) == 0 {
		return nil
	}

	return JobSequence(j...)
}

// Returns the attached commands to this task as a job to run as parallel depending on their definition order.
func (t *Task[Pipe]) GetCommandJobAsJobParallel() Job {
	j := t.GetCommandJobs()

	if len(j) == 0 {
		return nil
	}

	return JobParallel(j...)
}

// Attaches existing commands to this task.
func (t *Task[Pipe]) AddCommands(commands ...*Command[Pipe]) *Task[Pipe] {
	t.taskLock.Lock()
	t.commands = append(t.commands, commands...)
	t.taskLock.Unlock()

	return t
}

// Runs the commands that are attached to this task as sequence.
func (t *Task[Pipe]) RunCommandJobAsJobSequence() error {
	return t.Plumber.RunJobs(t.GetCommandJobAsJobSequence())
}

// Runs the commands that are attached to this task as parallel.
func (t *Task[Pipe]) RunCommandJobAsJobParallel() error {
	return t.Plumber.RunJobs(t.GetCommandJobAsJobParallel())
}

// Runs the commands that are attached to this task as parallel with the given wrapper.
func (t *Task[Pipe]) RunCommandJob(fn TaskJobParserFn[Pipe]) error {
	return t.Plumber.RunJobs(fn(t))
}
