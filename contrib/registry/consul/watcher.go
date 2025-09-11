package consul

import (
	"context"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/go-kratos/kratos/v2/registry"
)

type watcher struct {
	event chan struct{}
	set   *serviceSet

	// for cancel
	ctx    context.Context
	cancel context.CancelFunc
}

// getRelevantGoroutineStacks 获取当前 goroutine 及相关 goroutine 的堆栈
func getRelevantGoroutineStacks() string {
	currentGID := getCurrentGoroutineID()

	// 获取所有 goroutine 的堆栈
	buf := make([]byte, 1024*1024) // 1MB 缓冲区
	n := runtime.Stack(buf, true)
	allStacks := string(buf[:n])

	// 筛选相关 goroutine
	var relevantStacks strings.Builder
	stacks := strings.Split(allStacks, "\n\n")

	for _, stack := range stacks {
		if stack == "" {
			continue
		}

		// 总是包含当前 goroutine
		if strings.Contains(stack, fmt.Sprintf("goroutine %d ", currentGID)) {
			relevantStacks.WriteString(stack)
			relevantStacks.WriteString("\n\n")
			continue
		}

		// 包含与当前 goroutine 相关的其他 goroutine
		if isRelatedToCurrent(stack, currentGID) {
			relevantStacks.WriteString(stack)
			relevantStacks.WriteString("\n\n")
		}
	}

	if relevantStacks.Len() == 0 {
		return "No relevant goroutines found"
	}

	return relevantStacks.String()
}

// getCurrentGoroutineID 获取当前 goroutine ID
func getCurrentGoroutineID() uint64 {
	b := make([]byte, 64)
	b = b[:runtime.Stack(b, false)]
	s := strings.TrimPrefix(string(b), "goroutine ")
	s = s[:strings.Index(s, " ")]
	id, _ := strconv.ParseUint(s, 10, 64)
	return id
}

// isRelatedToCurrent 判断 goroutine 是否与当前 goroutine 相关
func isRelatedToCurrent(stack string, currentGID uint64) bool {
	// 规则1：检查是否由当前 goroutine 创建
	if strings.Contains(stack, "created by") {
		if strings.Contains(stack, fmt.Sprintf("in goroutine %d", currentGID)) {
			return true
		}
	}

	// 规则2：检查是否在等待相同的资源（通道、互斥锁等）
	if strings.Contains(stack, "chan receive") ||
		strings.Contains(stack, "chan send") ||
		strings.Contains(stack, "select") ||
		strings.Contains(stack, "sync.Mutex") {
		return true
	}

	// 规则3：检查是否有共同的调用路径（Kratos 相关）
	commonPrefixes := []string{
		"github.com/go-kratos/kratos",
		"contrib/registry/consul",
		"transport/grpc",
		"registry.",
	}

	for _, prefix := range commonPrefixes {
		if strings.Contains(stack, prefix) {
			return true
		}
	}

	return false
}

// formatStack 优化堆栈输出格式
func formatStack(stack string) string {
	if stack == "No relevant goroutines found" {
		return stack
	}

	return "======================= RELEVANT STACK TRACE =======================\n" +
		stack +
		"\n==========================================================="
}

func (w *watcher) Next() (services []*registry.ServiceInstance, err error) {
	if err = w.ctx.Err(); err != nil {
		return
	}

	select {
	case <-w.ctx.Done():
		err = w.ctx.Err()
		return
	case <-w.event:
	}

	ss, ok := w.set.services.Load().([]*registry.ServiceInstance)
	if ok {
		services = append(services, ss...)
	}
	return
}

func (w *watcher) Stop() error {
	now := time.Now().Format(time.RFC3339)
	gid := getCurrentGoroutineID()

	// 获取相关 goroutine 的堆栈
	relevantStacks := getRelevantGoroutineStacks()

	// 打印信息
	fmt.Printf(`
[%s] [Goroutine-%d] Watcher stopped
--- Relevant Goroutines Stack Trace ---
%s
`, now, gid, formatStack(relevantStacks))

	if w.cancel != nil {
		w.cancel()
		w.cancel = nil
		w.set.delete(w)
	}
	return nil
}
