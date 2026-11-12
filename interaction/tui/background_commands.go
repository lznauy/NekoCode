package tui

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"nekocode/interaction/tui/components/message"
	controlruntime "nekocode/runtime"
)

type backgroundController interface {
	CurrentSessionID() string
	BackgroundTasks(string) ([]controlruntime.BackgroundTask, error)
	AppendBackgroundTask(context.Context, string, string, string) error
	CancelBackgroundTask(context.Context, string, string) error
}

func (m *Model) backgroundCommand(value string) (bool, tea.Cmd) {
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return false, nil
	}
	command := fields[0]
	if command != "/task-review" && command != "/task-find" && command != "/task-export" && command != "/task-add" && command != "/task-stop" && command != "/background" && command != "/tasks-stop" && command != "/inbox" && command != "/demand-resume" && command != "/demand-discard" {
		return false, nil
	}
	rt, ok := m.Runtime.(backgroundController)
	if !ok {
		return false, nil
	}
	var out string
	var err error
	switch command {
	case "/task-review":
		controller, ok := m.Runtime.(interface {
			RetryBackgroundNotification(context.Context, string, string) error
		})
		if !ok || len(fields) != 2 {
			out = "用法：/task-review <任务 ID>"
			break
		}
		err = controller.RetryBackgroundNotification(context.Background(), rt.CurrentSessionID(), fields[1])
		out = "验收已重新排队；不会重新执行任务。"
	case "/task-find":
		controller, ok := m.Runtime.(interface {
			SearchSubagentOutputs(context.Context, string, string, string, int, int) (controlruntime.TaskSearchPage, error)
		})
		if !ok || len(fields) < 3 {
			out = "用法：/task-find <任务 ID> [--after=序号] <关键词>"
			break
		}
		after := 0
		query := fields[2:]
		if strings.HasPrefix(query[0], "--after=") {
			after, err = strconv.Atoi(strings.TrimPrefix(query[0], "--after="))
			query = query[1:]
			if err != nil || after < 0 || len(query) == 0 {
				out = "用法：/task-find <任务 ID> [--after=序号] <关键词>"
				err = nil
				break
			}
		}
		var page controlruntime.TaskSearchPage
		page, err = controller.SearchSubagentOutputs(context.Background(), rt.CurrentSessionID(), fields[1], strings.Join(query, " "), after, 20)
		for _, hit := range page.Hits {
			out += fmt.Sprintf("#%d %s\n", hit.Sequence, hit.Preview)
		}
		if page.HasMore {
			out += fmt.Sprintf("更多结果：/task-find %s --after=%d %s", fields[1], page.Next, strings.Join(query, " "))
		}
		if out == "" {
			out = "没有匹配输出。"
		}
	case "/task-export":
		controller, ok := m.Runtime.(interface {
			ExportSubagentOutputs(context.Context, string, string, io.Writer) error
		})
		if !ok || len(fields) != 3 {
			out = "用法：/task-export <任务 ID> <新文件路径>"
			break
		}
		var file *os.File
		file, err = os.OpenFile(fields[2], os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err == nil {
			err = controller.ExportSubagentOutputs(context.Background(), rt.CurrentSessionID(), fields[1], file)
			closeErr := file.Close()
			if err == nil {
				err = closeErr
			}
			if err != nil {
				_ = os.Remove(fields[2])
			}
		}
		out = "任务输出已导出到 " + fields[2]

	case "/inbox", "/demand-resume", "/demand-discard":
		inbox, supported := m.Runtime.(interface {
			BackgroundDemands(string) ([]controlruntime.BackgroundDemand, error)
			ResumeBackgroundDemand(context.Context, string, controlruntime.RunID) error
			DiscardBackgroundDemand(context.Context, string, controlruntime.RunID) error
		})
		if !supported {
			out = "需求收件箱不可用"
			break
		}
		if command == "/inbox" {
			var demands []controlruntime.BackgroundDemand
			demands, err = inbox.BackgroundDemands(rt.CurrentSessionID())
			var lines []string
			for _, d := range demands {
				lines = append(lines, fmt.Sprintf("%s  %s  %s", d.ID, d.Status, d.Input.Text))
			}
			out = strings.Join(lines, "\n")
			if out == "" {
				out = "暂无排队需求记录"
			}
		} else {
			if len(fields) != 2 {
				out = "用法：" + command + " <需求 ID>"
				break
			}
			if command == "/demand-resume" {
				err = inbox.ResumeBackgroundDemand(context.Background(), rt.CurrentSessionID(), controlruntime.RunID(fields[1]))
				out = "需求已重新排队。"
			} else {
				err = inbox.DiscardBackgroundDemand(context.Background(), rt.CurrentSessionID(), controlruntime.RunID(fields[1]))
				out = "已忽略需求记录。"
			}
		}

	case "/tasks-stop":
		controller, supported := m.Runtime.(interface {
			StopBackgroundTasks(context.Context, string) error
		})
		if !supported {
			out = "停止全部任务不可用"
			break
		}
		err = controller.StopBackgroundTasks(context.Background(), rt.CurrentSessionID())
		out = "已请求停止协调轮次、后台任务和排队需求。"
	case "/background":
		var tasks []controlruntime.BackgroundTask
		tasks, err = rt.BackgroundTasks(rt.CurrentSessionID())
		var lines []string
		for _, task := range tasks {
			lines = append(lines, fmt.Sprintf("%s  %s  %s · 待接收追加 %d · 验收 %s (%d 次) · %d tokens %s", task.ID, taskStatus(task.Status), task.Title, len(task.PendingUpdates), task.Delivery, task.DeliveryAttempts, task.Usage.TotalTokens, task.DeliveryError))
		}
		out = strings.Join(lines, "\n")
		if out == "" {
			out = "暂无后台任务"
		}
	case "/task-add":
		if len(fields) < 3 {
			out = "用法：/task-add <任务 ID> <追加内容>"
			break
		}
		text := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(value), command)), fields[1]))
		err = rt.AppendBackgroundTask(context.Background(), rt.CurrentSessionID(), fields[1], text)
		out = "追加内容已排队，子任务将在下一轮请求前接收。"
	case "/task-stop":
		if len(fields) != 2 {
			out = "用法：/task-stop <任务 ID>"
			break
		}
		err = rt.CancelBackgroundTask(context.Background(), rt.CurrentSessionID(), fields[1])
		out = "已请求停止该子任务。"
	}
	if err != nil {
		out = err.Error()
	}
	m.Messages.AddMessage(message.ChatMessage{Role: "system", Content: out, RenderedContent: out})
	m.Messages.GotoBottom()
	return true, nil
}
