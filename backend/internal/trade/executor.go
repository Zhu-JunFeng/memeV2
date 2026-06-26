package trade

import (
	"context"
	"encoding/json"
	"time"
)

// JupiterExecutor 先把执行入口固化成独立实现，
// 这样后面补真实签名与 RPC 细节时，不需要改动信号、回测或 Web API 模块。
type JupiterExecutor struct{}

func NewJupiterExecutor() *JupiterExecutor {
	return &JupiterExecutor{}
}

func (e *JupiterExecutor) Execute(context.Context, ExecutionRequest) (ExecutionResult, error) {
	return ExecutionResult{
		RequestPayload:  json.RawMessage(`{"provider":"jupiter"}`),
		ResponsePayload: json.RawMessage(`{"status":"not_ready"}`),
		ExecutedAt:      time.Now().UTC(),
	}, ErrTradeExecutionNotReady
}
