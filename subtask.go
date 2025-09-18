package plumber

import (
	"fmt"
)

type (
	SubtaskExtendFn func(pt *Task, st *Task)
)

// Creates a subtask that is attached to the current task.
func (t *Task) CreateSubtask(name ...string) *Task {
	parsed := append([]string{t.Name}, name...)

	st := NewTask(t.TL, parsed...)

	st.parent = t
	st.Lock = t.Lock

	return st
}

// Checks whether this task has a parent task.
func (t *Task) HasParent() bool {
	return t.parent != nil
}

// Extends the subtask of the current task with a wrapper.
func (t *Task) ExtendSubtask(fn JobFn) *Task {
	t.taskLock.Lock()
	t.subtask = fn(t.subtask)
	t.taskLock.Unlock()

	return t
}

// Attaches this task to a arbitatary given parent task.
func (t *Task) ToParent(
	parent *Task,
	fn SubtaskExtendFn,
) *Task {
	t.parent.taskLock.Lock()

	fn(parent, t)

	t.parent.taskLock.Unlock()

	return t
}

// Attaches this task to the parent task with a wrapper.
func (t *Task) AddSelfToTheParent(
	fn SubtaskExtendFn,
) *Task {
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
func (t *Task) AddSelfToTheParentAsSequence() *Task {
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
func (t *Task) AddSelfToTheParentAsParallel() *Task {
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
func (t *Task) GetSubtasks() Job {
	return t.subtask
}

// Sets the subtask of this task directly.
func (t *Task) SetSubtask(job Job) *Task {
	if job == nil {
		job = CreateEmptyJob()
	}

	t.taskLock.Lock()
	t.subtask = job
	t.taskLock.Unlock()

	return t
}

// Runs the subtasks of the current task.
func (t *Task) RunSubtasks() error {
	err := t.Plumber.RunJobs(t.subtask)

	if err == nil {
		t.SetSubtask(nil)
	}

	return err
}
