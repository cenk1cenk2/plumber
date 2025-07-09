package plumber

import (
	"fmt"
)

type (
	SubtaskExtendFn[Pipe TaskListData] func(pt *Task[Pipe], st *Task[Pipe])
)

// Creates a subtask that is attached to the current task.
func (t *Task[Pipe]) CreateSubtask(name ...string) *Task[Pipe] {
	parsed := append([]string{t.Name}, name...)

	st := NewTask(t.TL, parsed...)

	st.parent = t
	st.Lock = t.Lock

	return st
}

// Checks whether this task has a parent task.
func (t *Task[Pipe]) HasParent() bool {
	return t.parent != nil
}

// Extends the subtask of the current task with a wrapper.
func (t *Task[Pipe]) ExtendSubtask(fn JobFn) *Task[Pipe] {
	t.taskLock.Lock()
	t.subtask = fn(t.subtask)
	t.taskLock.Unlock()

	return t
}

// Attaches this task to a arbitatary given parent task.
func (t *Task[Pipe]) ToParent(
	parent *Task[Pipe],
	fn SubtaskExtendFn[Pipe],
) *Task[Pipe] {
	t.parent.taskLock.Lock()

	fn(parent, t)

	t.parent.taskLock.Unlock()

	return t
}

// Attaches this task to the parent task with a wrapper.
func (t *Task[Pipe]) AddSelfToTheParent(
	fn SubtaskExtendFn[Pipe],
) *Task[Pipe] {
	if !t.HasParent() {
		t.SendFatal(fmt.Errorf("Task has no parent value set."))

		return t
	}

	t.parent.Lock.Lock()
	fn(t.parent, t)
	t.parent.Lock.Unlock()

	return t
}

// Attaches this task to the parent task in sequence.
func (t *Task[Pipe]) AddSelfToTheParentAsSequence() *Task[Pipe] {
	if !t.HasParent() {
		t.SendFatal(fmt.Errorf("Task has no parent value set."))

		return t
	}

	t.parent.Lock.Lock()
	t.parent.ExtendSubtask(func(job Job) Job {
		return JobSequence(job, t.Job())
	})
	t.parent.Lock.Unlock()

	return t
}

// Attaches this task to the parent task in parallel.
func (t *Task[Pipe]) AddSelfToTheParentAsParallel() *Task[Pipe] {
	if !t.HasParent() {
		t.SendFatal(fmt.Errorf("Task has no parent value set."))

		return t
	}

	t.parent.Lock.Lock()
	t.parent.ExtendSubtask(func(job Job) Job {
		return JobParallel(job, t.Job())
	})
	t.parent.Lock.Unlock()

	return t
}

// Returns the subtasks of this task.
func (t *Task[Pipe]) GetSubtasks() Job {
	return t.subtask
}

// Sets the subtask of this task directly.
func (t *Task[Pipe]) SetSubtask(job Job) *Task[Pipe] {
	if job == nil {
		job = t.emptyJob
	}

	t.taskLock.Lock()
	t.subtask = job
	t.taskLock.Unlock()

	return t
}

// Runs the subtasks of the current task.
func (t *Task[Pipe]) RunSubtasks() error {
	err := t.Plumber.RunJobs(t.subtask)

	if err == nil {
		t.SetSubtask(nil)
	}

	return err
}
