package tui

func taskQueueReason(reason string) string {
	switch reason {
	case "waiting_dependencies":
		return "等待前置任务"
	case "foreground_writer":
		return "等待主任务释放工作区"
	case "workspace_busy":
		return "等待同工作区任务"
	case "concurrency_limit":
		return "并发名额已满"
	case "dispatch_pending":
		return "等待调度"
	}
	if reason != "" {
		return "等待调度（" + reason + "）"
	}
	return ""
}

func taskSchedulingNote(note string) string {
	switch note {
	case "uncommitted_changes":
		return "当前目录有未提交改动，使用共享工作区"
	case "repository_unavailable":
		return "无法确认 Git 提交与工作区状态，使用共享工作区"
	case "working_directory_untracked":
		return "当前工作目录不在 Git 提交中，使用共享工作区"
	case "submodules":
		return "项目包含 Git 子模块，使用共享工作区"
	case "index_flags":
		return "Git 索引设置可能隐藏本地改动，使用共享工作区"
	case "index_conflict":
		return "Git 索引存在未解决冲突，使用共享工作区"
	case "workspace_snapshot":
		return "独立快照包含已暂存、未暂存及未忽略的新文件"
	case "explicit_shared":
		return "派发时明确选择了共享工作区"
	case "mcp_requires_shared":
		return "所需 MCP 工具使用共享工作区"
	}
	if note != "" {
		return "工作区说明：" + note
	}
	return ""
}
