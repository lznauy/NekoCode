package taskbridge

import (
	"context"
	"nekocode/protocol"
)

// ResultNotReadyError reports a valid task whose selected attempt has not
// reached a terminal state yet. Callers can surface this as status, not as a
// failed tool execution.
type ResultNotReadyError = protocol.ResultNotReadyError

const MaxResultPage = 2000

type ResultQuery = protocol.ResultQuery
type ResultPage = protocol.ResultPage

type ResultReader func(context.Context, ResultQuery) (ResultPage, error)
