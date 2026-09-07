# plumber

## Description

A library for creating quick and effective CLI applications that have the process management build inside it.

---

<!-- toc -->

- [API Documentation](#api-documentation)
- [Migration to v7](#migration-to-v7)

<!-- tocstop -->

---

## API Documentation

You can find the API documentation [here](https://pkg.go.dev/github.com/cenk1cenk2/plumber/v7).

## Migration to v7

v7 replaces `go-floc` with an in-repo orchestration that is built on the standard library, and `logrus` with `log/slog`. Migration from v6 is mechanical.

- Update the import path to `github.com/cenk1cenk2/plumber/v7`.
- `Job` is now `func(ctx context.Context) error`. Drop the `floc.Context` and `floc.Control` parameters, return the error plainly and stop the flow through the cancellation of the context instead of `ctrl.Fail`.
- Callbacks take a context: `TaskFn`, `TaskListFn` and `CommandFn` are `func(ctx context.Context, x *X) error`. The builders, the predicates and the wrappers of them, `TaskListJobFn`, `TaskPredicateFn` and `TaskJobWrapperFn`, stay without a context.
- `Run`, `RunWith`, `RunBefore`, `RunAfter`, `RunSubtasks`, `RunCommandJob`, `RunCommandJobAsJobSequence` and `RunCommandJobAsJobParallel` take the context as their first argument. Run a flow with `RunJobs(job)` under the root context of the application, or with `RunJobsWith(ctx, job)` under a context of your own.
- `GuardResume(job)` no longer takes result masks and swallows every error of the job. `GuardResume(TASK_CANCELLED, job)` becomes `GuardIgnoreCancel(job)`, which only swallows the errors that are caused by the cancellation. `GuardAlways(job, grace...)` runs the job even when the context is already cancelled and takes an optional grace duration.
- The `TASK_*` result masks and `NewJobResultMask` are removed in favor of `errors.Is` and `context.Cause`. The shutdown of the application is `ErrShutdown`.
- `CreateBasicJob` becomes `CreateJob`, `CreateJobWithContext` is obsolete since every job has a context, and `JobThen` and `JobElse` are removed in favor of `JobSequence` and `JobIf`.
- The capture API, `Capture` and `Result[T]`, is removed. Pass the values with closures that write in to the typed fields of your own structs, where the order of `JobSequence` and the join of `JobParallel` are what make them readable after the flow.
- The loggers are `*slog.Logger` instead of `*logrus.Logger` and `*logrus.Entry`, therefore they have no printf methods anymore. Port the call sites by wrapping the message with `fmt.Sprintf`, `log.Infof("%s", name)` becomes `log.Info(fmt.Sprintf("%s", name))`, and log the level that slog does not know about with `log.Log(ctx, logger.LevelTrace, message)`.
- `LogLevel` is an enum of its own that is parsed with `ParseLogLevel` and stays the level of the configuration boundary, of the deprecation notices and of `Command.SetLogLevel`. `WithField` and `WithFields` become `With` with the attributes of slog, `SetFormatter` becomes `SetLoggerOptions`, and the controls of the root logger are `SetLoggerLevel`, `SetLoggerOutput` and `SetLoggerReportCaller` on the application together with the getters of them.
- `AppChannel` and the exported `Lock` of the terminator are removed. The root context of the application together with `SendError`, `SendFatal` and `SendExit` replace them, and `SetExitFunc` overrides how the process exits.
- `CombineTaskLists` takes anything that implements `TaskLister`, and `Jobber` is the interface for anything that produces a `Job`.
- The `MARKDOWN_DOC` and `MARKDOWN_EMBED` pseudo-commands are deprecated and still work as they did. Opt in to the `docs` command with `Commands: []*cli.Command{plumber.DocsCommand(p)}` and generate the documentation with `docs markdown` or `docs embed`, where `--output` overrides the file of `SetDocumentationOptions` for that run.
