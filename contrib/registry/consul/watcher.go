package consul

import (
	"context"
	"fmt"
	"runtime"
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

// getStackTrace 获取当前调用堆栈
func getStackTrace(skip int) string {
	buf := make([]byte, 4096)
	for {
		n := runtime.Stack(buf, false)
		if n < len(buf) {
			return formatStack(buf[:n], skip)
		}
		buf = make([]byte, 2*len(buf))
	}
}

// formatStack 格式化堆栈输出，跳过指定层数
func formatStack(buf []byte, skip int) string {
	stack := string(buf)
	lines := strings.Split(stack, "\n")

	// 跳过 runtime 调用
	var filtered []string
	count := 0
	for i := 0; i < len(lines); i += 2 {
		if count >= skip && i+1 < len(lines) {
			filtered = append(filtered, lines[i], lines[i+1])
		}
		count++
	}

	return strings.Join(filtered, "\n")
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
	// 在停止时打印堆栈跟踪
	stack := getStackTrace(2) // 跳过2层调用
	fmt.Printf("[%s] Watcher stopped\nStack trace:\n%s\n",
		time.Now().Format(time.RFC3339), stack)

	if w.cancel != nil {
		w.cancel()
		w.cancel = nil
		w.set.delete(w)
	}
	return nil
}
