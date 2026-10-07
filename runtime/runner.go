package runtime

import (
	"context"

	"nekocode/protocol"
)

// Runner is the only capability required by Runtime.
//
// A runner receives a host scoped to exactly one run. It must not retain the
// host after Run returns. A Runner or command callback must not call
// Runtime.Close synchronously; shutdown is owned by the application lifecycle.
type Runner interface {
	Run(ctx context.Context, input string, host RunHost) (string, error)
}

// RunnerFunc adapts a function into a Runner for small applications.
type RunnerFunc func(context.Context, string, RunHost) (string, error)

func (f RunnerFunc) Run(ctx context.Context, input string, host RunHost) (string, error) {
	return f(ctx, input, host)
}

// RunHost is the bot-facing boundary for streaming output and requesting
// interaction during one run. Confirm and Ask are serialized so interaction
// surfaces only need to present one pending request at a time.
type RunHost interface {
	Text(delta string)
	Reason(delta string)
	Step(event protocol.StepEvent)
	Phase(phase string)
	Todos(items []TodoItem)
	Confirm(request ConfirmRequest) ConfirmReply
	Ask(request QuestionRequest) QuestionReply
}

// RunImageSource is an optional RunHost extension implemented by the
// runtime's own host. Runners that support native image input retrieve this
// run's attached images through it instead of a Runner signature change.
type RunImageSource interface {
	RunImages() []ImageAttachment
}

// CommandAction describes what runtime should do after a bot command.
type CommandAction = protocol.CommandAction

const (
	CommandIgnored  = protocol.CommandIgnored
	CommandHandled  = protocol.CommandHandled
	CommandContinue = protocol.CommandContinue
)

type CommandResult = protocol.CommandResult
type CommandMenu = protocol.CommandMenu
type CommandMenuItem = protocol.CommandMenuItem
