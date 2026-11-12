package agentprofile

import (
	"context"
	"fmt"

	"nekocode/mode"
)

// Executor is resolved from trusted registrations by the application. It is
// deliberately not a declarative base with an implicit all-tools wildcard.
func Executor(tools []string) Profile {
	return Profile{Name: "executor", Description: "General task execution with captured delegated capabilities; Super Agent only", RequiredRole: mode.Coordinator, SystemPrompt: "任务要求的关键工具如果不在实际可见工具中，应立即提交 partial 结果，明确缺少什么工具、哪些步骤未执行；不要为此反复尝试无关工具或请求不存在工具的审批。只读审查可以需要 shell 来运行 git 和测试；缺少 shell 不是审批模式的问题。", Tools: append([]string(nil), tools...)}
}

// CheckSource validates host-captured origin, before creating or restoring a
// worker. A model argument or a saved profile cannot grant a coordinator role.
func (p Profile) CheckSource(ctx context.Context) error {
	if p.RequiredRole == "" {
		return nil
	}
	snapshot, ok := mode.FromContext(ctx)
	if !ok {
		return fmt.Errorf("profile %q requires %s origin", p.Name, p.RequiredRole)
	}
	if _, err := mode.Validate(snapshot); err != nil {
		return err
	}
	if snapshot.Role != p.RequiredRole {
		return fmt.Errorf("profile %q requires %s origin", p.Name, p.RequiredRole)
	}
	return nil
}
