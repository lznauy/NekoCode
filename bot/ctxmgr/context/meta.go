// meta.go — 系统元数据格式化。所有注入上下文的系统标注统一在此定义，
// 保证与工具输出结构一致的 <tag>value</tag> 格式。

package context

import "fmt"

// FormatEnv 格式化环境信息（工作目录、日期）。
func FormatEnv(cwd, date string) string {
	return fmt.Sprintf("<env>\n<cwd>%s</cwd>\n<date>%s</date>\n</env>", cwd, date)
}

// FormatCwd 格式化工作目录（子代理等场景）。
func FormatCwd(cwd string) string {
	return fmt.Sprintf("<cwd>%s</cwd>", cwd)
}


// FormatTodo 格式化任务列表。
func FormatTodo(todo string) string {
	return fmt.Sprintf("<todo>%s</todo>", todo)
}
